// Copyright 2015 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package notify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

// A Notifier notifies about alerts under constraints of the given context.
// It returns an error if unsuccessful and a flag whether the error is
// recoverable. This information is useful for a retry logic.
type Notifier interface {
	Notify(context.Context, ...*types.Alert) (bool, error)
}

// An Integration wraps a notifier and its config to be uniquely identified by
// name and index from its origin in the configuration.
type Integration struct {
	notifier Notifier
	conf     notifierConfig
	name     string
	idx      int
}

// Notify implements the Notifier interface.
func (i *Integration) Notify(ctx context.Context, alerts ...*types.Alert) (bool, error) {
	return i.notifier.Notify(ctx, alerts...)
}

// BuildReceiverIntegrations builds a list of integration notifiers off of a
// receivers config.
func BuildReceiverIntegrations(nc *config.Receiver, tmpl *template.Template, logger log.Logger) []Integration {
	var (
		integrations []Integration
		add          = func(name string, i int, n Notifier, nc notifierConfig) {
			integrations = append(integrations, Integration{
				notifier: n,
				conf:     nc,
				name:     name,
				idx:      i,
			})
		}
	)

	for i, c := range nc.WebhookConfigs {
		n := NewWebhook(c, tmpl, logger)
		add("webhook", i, n, c)
	}
	for i, c := range nc.EmailConfigs {
		n := NewEmail(c, tmpl, logger)
		add("email", i, n, c)
	}
	for i, c := range nc.PagerdutyConfigs {
		n := NewPagerDuty(c, tmpl, logger)
		add("pagerduty", i, n, c)
	}
	for i, c := range nc.OpsGenieConfigs {
		n := NewOpsGenie(c, tmpl, logger)
		add("opsgenie", i, n, c)
	}
	for i, c := range nc.WechatConfigs {
		n := NewWechat(c, tmpl, logger)
		add("wechat", i, n, c)
	}
	for i, c := range nc.SlackConfigs {
		n := NewSlack(c, tmpl, logger)
		add("slack", i, n, c)
	}
	for i, c := range nc.HipchatConfigs {
		n := NewHipchat(c, tmpl, logger)
		add("hipchat", i, n, c)
	}
	for i, c := range nc.VictorOpsConfigs {
		n := NewVictorOps(c, tmpl, logger)
		add("victorops", i, n, c)
	}
	for i, c := range nc.PushoverConfigs {
		n := NewPushover(c, tmpl, logger)
		add("pushover", i, n, c)
	}
	return integrations
}

const contentTypeJSON = "application/json"

var userAgentHeader = fmt.Sprintf("Alertmanager/%s", version.Version)

// Webhook implements a Notifier for generic webhooks.
type Webhook struct {
	conf   *config.WebhookConfig
	tmpl   *template.Template
	logger log.Logger
}

// NewWebhook returns a new Webhook.
func NewWebhook(conf *config.WebhookConfig, t *template.Template, l log.Logger) *Webhook {
	return &Webhook{conf: conf, tmpl: t, logger: l}
}

// WebhookMessage defines the JSON object send to webhook endpoints.
type WebhookMessage struct {
	*template.Data

	// The protocol version.
	Version  string `json:"version"`
	GroupKey string `json:"groupKey"`
}

// Notify implements the Notifier interface.
func (w *Webhook) Notify(ctx context.Context, alerts ...*types.Alert) (bool, error) {
	data := w.tmpl.Data(receiverName(ctx, w.logger), groupLabels(ctx, w.logger), alerts...)

	groupKey, ok := GroupKey(ctx)
	if !ok {
		level.Error(w.logger).Log("msg", "group key missing")
	}

	msg := &WebhookMessage{
		Version:  "4",
		Data:     data,
		GroupKey: groupKey,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return false, err
	}

	req, err := http.NewRequest("POST", w.conf.URL.String(), &buf)
	if err != nil {
		return true, err
	}
	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("User-Agent", userAgentHeader)

	c, err := commoncfg.NewClientFromConfig(*w.conf.HTTPConfig, "webhook")
	if err != nil {
		return false, err
	}

	resp, err := c.Do(req.WithContext(ctx))
	if err != nil {
		return true, err
	}
	resp.Body.Close()

	return w.retry(resp.StatusCode)
}

func (w *Webhook) retry(statusCode int) (bool, error) {
	// Webhooks are assumed to respond with 2xx response codes on a successful
	// request and 5xx response codes are assumed to be recoverable.
	if statusCode/100 != 2 {
		return (statusCode/100 == 5), fmt.Errorf("unexpected status code %v from %s", statusCode, w.conf.URL)
	}

	return false, nil
}

// PagerDuty implements a Notifier for PagerDuty notifications.
type PagerDuty struct {
	conf   *config.PagerdutyConfig
	tmpl   *template.Template
	logger log.Logger
	apiV1  string // for tests.
}

// NewPagerDuty returns a new PagerDuty notifier.
func NewPagerDuty(c *config.PagerdutyConfig, t *template.Template, l log.Logger) *PagerDuty {
	n := &PagerDuty{conf: c, tmpl: t, logger: l}
	if c.ServiceKey != "" {
		n.apiV1 = "https://events.pagerduty.com/generic/2010-04-15/create_event.json"
	}
	return n
}

const (
	pagerDutyEventTrigger = "trigger"
	pagerDutyEventResolve = "resolve"
)

type pagerDutyMessage struct {
	RoutingKey  string            `json:"routing_key,omitempty"`
	ServiceKey  string            `json:"service_key,omitempty"`
	DedupKey    string            `json:"dedup_key,omitempty"`
	IncidentKey string            `json:"incident_key,omitempty"`
	EventType   string            `json:"event_type,omitempty"`
	Description string            `json:"description,omitempty"`
	EventAction string            `json:"event_action"`
	Payload     *pagerDutyPayload `json:"payload"`
	Client      string            `json:"client,omitempty"`
	ClientURL   string            `json:"client_url,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
	Images      []pagerDutyImage  `json:"images,omitempty"`
	Links       []pagerDutyLink   `json:"links,omitempty"`
}

type pagerDutyLink struct {
	HRef string `json:"href"`
	Text string `json:"text"`
}

type pagerDutyImage struct {
	Src  string `json:"src"`
	Alt  string `json:"alt"`
	Text string `json:"text"`
}

type pagerDutyPayload struct {
	Summary       string            `json:"summary"`
	Source        string            `json:"source"`
	Severity      string            `json:"severity"`
	Timestamp     string            `json:"timestamp,omitempty"`
	Class         string            `json:"class,omitempty"`
	Component     string            `json:"component,omitempty"`
	Group         string            `json:"group,omitempty"`
	CustomDetails map[string]string `json:"custom_details,omitempty"`
}

func (n *PagerDuty) notifyV1(
	ctx context.Context,
	c *http.Client,
	eventType, key string,
	data *template.Data,
	details map[string]string,
	as ...*types.Alert,
) (bool, error) {
	var tmplErr error
	tmpl := tmplText(n.tmpl, data, &tmplErr)

	msg := &pagerDutyMessage{
		ServiceKey:  tmpl(string(n.conf.ServiceKey)),
		EventType:   eventType,
		IncidentKey: hashKey(key),
		Description: tmpl(n.conf.Description),
		Details:     details,
	}

	if eventType == pagerDutyEventTrigger {
		msg.Client = tmpl(n.conf.Client)
		msg.ClientURL = tmpl(n.conf.ClientURL)
	}

	if tmplErr != nil {
		return false, fmt.Errorf("failed to template PagerDuty v1 message: %v", tmplErr)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return false, err
	}

	resp, err := post(ctx, c, n.apiV1, contentTypeJSON, &buf)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()

	return n.retryV1(resp)
}

func (n *PagerDuty) notifyV2(
	ctx context.Context,
	c *http.Client,
	eventType, key string,
	data *template.Data,
	details map[string]string,
	as ...*types.Alert,
) (bool, error) {
	var tmplErr error
	tmpl := tmplText(n.tmpl, data, &tmplErr)

	if n.conf.Severity == "" {
		n.conf.Severity = "error"
	}

	summary := tmpl(n.conf.Description)
	summaryRunes := []rune(summary)
	if len(summaryRunes) > 1024 {
		summary = string(summaryRunes[:1018]) + " [...]"
	}

	msg := &pagerDutyMessage{
		Client:      tmpl(n.conf.Client),
		ClientURL:   tmpl(n.conf.ClientURL),
		RoutingKey:  tmpl(string(n.conf.RoutingKey)),
		EventAction: eventType,
		DedupKey:    hashKey(key),
		Images:      make([]pagerDutyImage, len(n.conf.Images)),
		Links:       make([]pagerDutyLink, len(n.conf.Links)),
		Payload: &pagerDutyPayload{
			Summary:       summary,
			Source:        tmpl(n.conf.Client),
			Severity:      tmpl(n.conf.Severity),
			CustomDetails: details,
			Class:         tmpl(n.conf.Class),
			Component:     tmpl(n.conf.Component),
			Group:         tmpl(n.conf.Group),
		},
	}

	for index, item := range n.conf.Images {
		msg.Images[index].Src = tmpl(item.Src)
		msg.Images[index].Alt = tmpl(item.Alt)
		msg.Images[index].Text = tmpl(item.Text)
	}

	for index, item := range n.conf.Links {
		msg.Links[index].HRef = tmpl(item.HRef)
		msg.Links[index].Text = tmpl(item.Text)
	}

	if tmplErr != nil {
		return false, fmt.Errorf("failed to template PagerDuty v2 message: %v", tmplErr)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return false, fmt.Errorf("failed to encode PagerDuty v2 message: %v", err)
	}

	resp, err := post(ctx, c, n.conf.URL.String(), contentTypeJSON, &buf)
	if err != nil {
		return true, fmt.Errorf("failed to post message to PagerDuty: %v", err)
	}
	defer resp.Body.Close()

	return n.retryV2(resp)
}

// Notify implements the Notifier interface.
//
// https://v2.developer.pagerduty.com/docs/events-api-v2
func (n *PagerDuty) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	key, ok := GroupKey(ctx)
	if !ok {
		return false, fmt.Errorf("group key missing")
	}

	var err error
	var (
		alerts    = types.Alerts(as...)
		data      = n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)
		eventType = pagerDutyEventTrigger
	)
	if alerts.Status() == model.AlertResolved {
		eventType = pagerDutyEventResolve
	}

	level.Debug(n.logger).Log("msg", "Notifying PagerDuty", "incident", key, "eventType", eventType)

	details := make(map[string]string, len(n.conf.Details))
	for k, v := range n.conf.Details {
		detail, err := n.tmpl.ExecuteTextString(v, data)
		if err != nil {
			return false, fmt.Errorf("failed to template %q: %v", v, err)
		}
		details[k] = detail
	}

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "pagerduty")
	if err != nil {
		return false, err
	}

	if n.apiV1 != "" {
		return n.notifyV1(ctx, c, eventType, key, data, details, as...)
	}
	return n.notifyV2(ctx, c, eventType, key, data, details, as...)
}

func pagerDutyErr(status int, body io.Reader) error {
	// See https://v2.developer.pagerduty.com/docs/trigger-events for the v1 events API.
	// See https://v2.developer.pagerduty.com/docs/send-an-event-events-api-v2 for the v2 events API.
	type pagerDutyResponse struct {
		Status  string   `json:"status"`
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	if status == http.StatusBadRequest && body != nil {
		var r pagerDutyResponse
		if err := json.NewDecoder(body).Decode(&r); err == nil {
			return fmt.Errorf("%s: %s", r.Message, strings.Join(r.Errors, ","))
		}
	}
	return fmt.Errorf("unexpected status code: %v", status)
}

func (n *PagerDuty) retryV1(resp *http.Response) (bool, error) {
	// Retrying can solve the issue on 403 (rate limiting) and 5xx response codes.
	// 2xx response codes indicate a successful request.
	// https://v2.developer.pagerduty.com/docs/trigger-events
	statusCode := resp.StatusCode

	if statusCode/100 != 2 {
		return (statusCode == http.StatusForbidden || statusCode/100 == 5), pagerDutyErr(statusCode, resp.Body)
	}
	return false, nil
}

func (n *PagerDuty) retryV2(resp *http.Response) (bool, error) {
	// Retrying can solve the issue on 429 (rate limiting) and 5xx response codes.
	// 2xx response codes indicate a successful request.
	// https://v2.developer.pagerduty.com/docs/events-api-v2#api-response-codes--retry-logic
	statusCode := resp.StatusCode

	if statusCode/100 != 2 {
		return (statusCode == http.StatusTooManyRequests || statusCode/100 == 5), pagerDutyErr(statusCode, resp.Body)
	}

	return false, nil
}

// Slack implements a Notifier for Slack notifications.
type Slack struct {
	conf   *config.SlackConfig
	tmpl   *template.Template
	logger log.Logger
}

// NewSlack returns a new Slack notification handler.
func NewSlack(c *config.SlackConfig, t *template.Template, l log.Logger) *Slack {
	return &Slack{
		conf:   c,
		tmpl:   t,
		logger: l,
	}
}

// slackReq is the request for sending a slack notification.
type slackReq struct {
	Channel     string            `json:"channel,omitempty"`
	Username    string            `json:"username,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	IconURL     string            `json:"icon_url,omitempty"`
	LinkNames   bool              `json:"link_names,omitempty"`
	Attachments []slackAttachment `json:"attachments"`
}

// slackAttachment is used to display a richly-formatted message block.
type slackAttachment struct {
	Title      string               `json:"title,omitempty"`
	TitleLink  string               `json:"title_link,omitempty"`
	Pretext    string               `json:"pretext,omitempty"`
	Text       string               `json:"text"`
	Fallback   string               `json:"fallback"`
	CallbackID string               `json:"callback_id"`
	Fields     []config.SlackField  `json:"fields,omitempty"`
	Actions    []config.SlackAction `json:"actions,omitempty"`
	ImageURL   string               `json:"image_url,omitempty"`
	ThumbURL   string               `json:"thumb_url,omitempty"`
	Footer     string               `json:"footer"`

	Color    string   `json:"color,omitempty"`
	MrkdwnIn []string `json:"mrkdwn_in,omitempty"`
}

// Notify implements the Notifier interface.
func (n *Slack) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	var err error
	var (
		data     = n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)
		tmplText = tmplText(n.tmpl, data, &err)
	)

	attachment := &slackAttachment{
		Title:      tmplText(n.conf.Title),
		TitleLink:  tmplText(n.conf.TitleLink),
		Pretext:    tmplText(n.conf.Pretext),
		Text:       tmplText(n.conf.Text),
		Fallback:   tmplText(n.conf.Fallback),
		CallbackID: tmplText(n.conf.CallbackID),
		ImageURL:   tmplText(n.conf.ImageURL),
		ThumbURL:   tmplText(n.conf.ThumbURL),
		Footer:     tmplText(n.conf.Footer),
		Color:      tmplText(n.conf.Color),
		MrkdwnIn:   []string{"fallback", "pretext", "text"},
	}

	var numFields = len(n.conf.Fields)
	if numFields > 0 {
		var fields = make([]config.SlackField, numFields)
		for index, field := range n.conf.Fields {
			// Check if short was defined for the field otherwise fallback to the global setting
			var short bool
			if field.Short != nil {
				short = *field.Short
			} else {
				short = n.conf.ShortFields
			}

			// Rebuild the field by executing any templates and setting the new value for short
			fields[index] = config.SlackField{
				Title: tmplText(field.Title),
				Value: tmplText(field.Value),
				Short: &short,
			}
		}
		attachment.Fields = fields
	}

	var numActions = len(n.conf.Actions)
	if numActions > 0 {
		var actions = make([]config.SlackAction, numActions)
		for index, action := range n.conf.Actions {
			slackAction := config.SlackAction{
				Type:  tmplText(action.Type),
				Text:  tmplText(action.Text),
				URL:   tmplText(action.URL),
				Style: tmplText(action.Style),
				Name:  tmplText(action.Name),
				Value: tmplText(action.Value),
			}

			if action.ConfirmField != nil {
				slackAction.ConfirmField = &config.SlackConfirmationField{
					Title:       tmplText(action.ConfirmField.Title),
					Text:        tmplText(action.ConfirmField.Text),
					OkText:      tmplText(action.ConfirmField.OkText),
					DismissText: tmplText(action.ConfirmField.DismissText),
				}
			}

			actions[index] = slackAction
		}
		attachment.Actions = actions
	}

	req := &slackReq{
		Channel:     tmplText(n.conf.Channel),
		Username:    tmplText(n.conf.Username),
		IconEmoji:   tmplText(n.conf.IconEmoji),
		IconURL:     tmplText(n.conf.IconURL),
		LinkNames:   n.conf.LinkNames,
		Attachments: []slackAttachment{*attachment},
	}
	if err != nil {
		return false, err
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return false, err
	}

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "slack")
	if err != nil {
		return false, err
	}

	u := n.conf.APIURL.String()
	resp, err := post(ctx, c, u, contentTypeJSON, &buf)
	if err != nil {
		return true, redactURL(err)
	}
	resp.Body.Close()

	return n.retry(resp.StatusCode)
}

func (n *Slack) retry(statusCode int) (bool, error) {
	// Only 5xx response codes are recoverable and 2xx codes are successful.
	// https://api.slack.com/incoming-webhooks#handling_errors
	// https://api.slack.com/changelog/2016-05-17-changes-to-errors-for-incoming-webhooks
	if statusCode/100 != 2 {
		return (statusCode/100 == 5), fmt.Errorf("unexpected status code %v", statusCode)
	}

	return false, nil
}

// Hipchat implements a Notifier for Hipchat notifications.
type Hipchat struct {
	conf   *config.HipchatConfig
	tmpl   *template.Template
	logger log.Logger
}

// NewHipchat returns a new Hipchat notification handler.
func NewHipchat(c *config.HipchatConfig, t *template.Template, l log.Logger) *Hipchat {
	return &Hipchat{
		conf:   c,
		tmpl:   t,
		logger: l,
	}
}

type hipchatReq struct {
	From          string `json:"from"`
	Notify        bool   `json:"notify"`
	Message       string `json:"message"`
	MessageFormat string `json:"message_format"`
	Color         string `json:"color"`
}

// Notify implements the Notifier interface.
func (n *Hipchat) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	var err error
	var msg string
	var (
		data     = n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)
		tmplText = tmplText(n.tmpl, data, &err)
		tmplHTML = tmplHTML(n.tmpl, data, &err)
		roomid   = tmplText(n.conf.RoomID)
		apiURL   = n.conf.APIURL.Copy()
	)
	apiURL.Path += fmt.Sprintf("v2/room/%s/notification", roomid)
	q := apiURL.Query()
	q.Set("auth_token", string(n.conf.AuthToken))
	apiURL.RawQuery = q.Encode()

	if n.conf.MessageFormat == "html" {
		msg = tmplHTML(n.conf.Message)
	} else {
		msg = tmplText(n.conf.Message)
	}

	req := &hipchatReq{
		From:          tmplText(n.conf.From),
		Notify:        n.conf.Notify,
		Message:       msg,
		MessageFormat: n.conf.MessageFormat,
		Color:         tmplText(n.conf.Color),
	}
	if err != nil {
		return false, err
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return false, err
	}

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "hipchat")
	if err != nil {
		return false, err
	}

	resp, err := post(ctx, c, apiURL.String(), contentTypeJSON, &buf)
	if err != nil {
		return true, redactURL(err)
	}

	defer resp.Body.Close()

	return n.retry(resp.StatusCode)
}

func (n *Hipchat) retry(statusCode int) (bool, error) {
	// Response codes 429 (rate limiting) and 5xx can potentially recover.
	// 2xx response codes indicate successful requests.
	// https://developer.atlassian.com/hipchat/guide/hipchat-rest-api/api-response-codes
	if statusCode/100 != 2 {
		return (statusCode == 429 || statusCode/100 == 5), fmt.Errorf("unexpected status code %v", statusCode)
	}

	return false, nil
}

// Wechat implements a Notfier for wechat notifications
type Wechat struct {
	conf   *config.WechatConfig
	tmpl   *template.Template
	logger log.Logger

	accessToken   string
	accessTokenAt time.Time
}

// Wechat AccessToken with corpid and corpsecret.
type WechatToken struct {
	AccessToken string `json:"access_token"`
}

type weChatMessage struct {
	Text    weChatMessageContent `yaml:"text,omitempty" json:"text,omitempty"`
	ToUser  string               `yaml:"touser,omitempty" json:"touser,omitempty"`
	ToParty string               `yaml:"toparty,omitempty" json:"toparty,omitempty"`
	Totag   string               `yaml:"totag,omitempty" json:"totag,omitempty"`
	AgentID string               `yaml:"agentid,omitempty" json:"agentid,omitempty"`
	Safe    string               `yaml:"safe,omitempty" json:"safe,omitempty"`
	Type    string               `yaml:"msgtype,omitempty" json:"msgtype,omitempty"`
}

type weChatMessageContent struct {
	Content string `json:"content"`
}

type weChatResponse struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
}

// NewWechat returns a new Wechat notifier.
func NewWechat(c *config.WechatConfig, t *template.Template, l log.Logger) *Wechat {
	return &Wechat{conf: c, tmpl: t, logger: l}
}

// Notify implements the Notifier interface.
func (n *Wechat) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	key, ok := GroupKey(ctx)
	if !ok {
		return false, fmt.Errorf("group key missing")
	}

	level.Debug(n.logger).Log("msg", "Notifying Wechat", "incident", key)
	data := n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)

	var err error
	tmpl := tmplText(n.tmpl, data, &err)
	if err != nil {
		return false, err
	}

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "wechat")
	if err != nil {
		return false, err
	}

	// Refresh AccessToken over 2 hours
	if n.accessToken == "" || time.Since(n.accessTokenAt) > 2*time.Hour {
		parameters := url.Values{}
		parameters.Add("corpsecret", tmpl(string(n.conf.APISecret)))
		parameters.Add("corpid", tmpl(string(n.conf.CorpID)))
		if err != nil {
			return false, fmt.Errorf("templating error: %s", err)
		}

		u := n.conf.APIURL.Copy()
		u.Path += "gettoken"
		u.RawQuery = parameters.Encode()

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return true, err
		}

		req.Header.Set("Content-Type", contentTypeJSON)

		resp, err := c.Do(req.WithContext(ctx))
		if err != nil {
			return true, redactURL(err)
		}
		defer resp.Body.Close()

		var wechatToken WechatToken
		if err := json.NewDecoder(resp.Body).Decode(&wechatToken); err != nil {
			return false, err
		}

		if wechatToken.AccessToken == "" {
			return false, fmt.Errorf("invalid APISecret for CorpID: %s", n.conf.CorpID)
		}

		// Cache accessToken
		n.accessToken = wechatToken.AccessToken
		n.accessTokenAt = time.Now()
	}

	msg := &weChatMessage{
		Text: weChatMessageContent{
			Content: tmpl(n.conf.Message),
		},
		ToUser:  tmpl(n.conf.ToUser),
		ToParty: tmpl(n.conf.ToParty),
		Totag:   tmpl(n.conf.ToTag),
		AgentID: tmpl(n.conf.AgentID),
		Type:    "text",
		Safe:    "0",
	}
	if err != nil {
		return false, fmt.Errorf("templating error: %s", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return false, err
	}

	postMessageURL := n.conf.APIURL.Copy()
	postMessageURL.Path += "message/send"
	q := postMessageURL.Query()
	q.Set("access_token", n.accessToken)
	postMessageURL.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, postMessageURL.String(), &buf)
	if err != nil {
		return true, err
	}

	resp, err := c.Do(req.WithContext(ctx))
	if err != nil {
		return true, redactURL(err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return true, err
	}
	level.Debug(n.logger).Log("msg", "response: "+string(body), "incident", key)

	if resp.StatusCode != 200 {
		return true, fmt.Errorf("unexpected status code %v", resp.StatusCode)
	}

	var weResp weChatResponse
	if err := json.Unmarshal(body, &weResp); err != nil {
		return true, err
	}

	// https://work.weixin.qq.com/api/doc#10649
	if weResp.Code == 0 {
		return false, nil
	}

	// AccessToken is expired
	if weResp.Code == 42001 {
		n.accessToken = ""
		return true, errors.New(weResp.Error)
	}

	return false, errors.New(weResp.Error)
}

// OpsGenie implements a Notifier for OpsGenie notifications.
type OpsGenie struct {
	conf   *config.OpsGenieConfig
	tmpl   *template.Template
	logger log.Logger
}

// NewOpsGenie returns a new OpsGenie notifier.
func NewOpsGenie(c *config.OpsGenieConfig, t *template.Template, l log.Logger) *OpsGenie {
	return &OpsGenie{conf: c, tmpl: t, logger: l}
}

type opsGenieCreateMessage struct {
	Alias       string              `json:"alias"`
	Message     string              `json:"message"`
	Description string              `json:"description,omitempty"`
	Details     map[string]string   `json:"details"`
	Source      string              `json:"source"`
	Teams       []map[string]string `json:"teams,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Note        string              `json:"note,omitempty"`
	Priority    string              `json:"priority,omitempty"`
}

type opsGenieCloseMessage struct {
	Source string `json:"source"`
}

// Notify implements the Notifier interface.
func (n *OpsGenie) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	req, retry, err := n.createRequest(ctx, as...)
	if err != nil {
		return retry, err
	}

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "opsgenie")
	if err != nil {
		return false, err
	}

	resp, err := c.Do(req.WithContext(ctx))

	if err != nil {
		return true, err
	}
	defer resp.Body.Close()

	return n.retry(resp.StatusCode)
}

// Like Split but filter out empty strings.
func safeSplit(s string, sep string) []string {
	a := strings.Split(strings.TrimSpace(s), sep)
	b := a[:0]
	for _, x := range a {
		if x != "" {
			b = append(b, x)
		}
	}
	return b
}

// Create requests for a list of alerts.
func (n *OpsGenie) createRequest(ctx context.Context, as ...*types.Alert) (*http.Request, bool, error) {
	key, ok := GroupKey(ctx)
	if !ok {
		return nil, false, fmt.Errorf("group key missing")
	}
	data := n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)

	level.Debug(n.logger).Log("msg", "Notifying OpsGenie", "incident", key)

	var err error
	tmpl := tmplText(n.tmpl, data, &err)

	details := make(map[string]string, len(n.conf.Details))
	for k, v := range n.conf.Details {
		details[k] = tmpl(v)
	}

	var (
		msg    interface{}
		apiURL = n.conf.APIURL.Copy()
		alias  = hashKey(key)
		alerts = types.Alerts(as...)
	)
	switch alerts.Status() {
	case model.AlertResolved:
		apiURL.Path += fmt.Sprintf("v2/alerts/%s/close", alias)
		q := apiURL.Query()
		q.Set("identifierType", "alias")
		apiURL.RawQuery = q.Encode()
		msg = &opsGenieCloseMessage{Source: tmpl(n.conf.Source)}
	default:
		message, truncated := truncate(tmpl(n.conf.Message), 130)
		if truncated {
			level.Debug(n.logger).Log("msg", "truncated message due to OpsGenie message limit", "truncated_message", message, "incident", key)
		}

		apiURL.Path += "v2/alerts"
		var teams []map[string]string
		for _, t := range safeSplit(string(tmpl(n.conf.Teams)), ",") {
			teams = append(teams, map[string]string{"name": t})
		}
		tags := safeSplit(string(tmpl(n.conf.Tags)), ",")

		msg = &opsGenieCreateMessage{
			Alias:       alias,
			Message:     message,
			Description: tmpl(n.conf.Description),
			Details:     details,
			Source:      tmpl(n.conf.Source),
			Teams:       teams,
			Tags:        tags,
			Note:        tmpl(n.conf.Note),
			Priority:    tmpl(n.conf.Priority),
		}
	}

	apiKey := tmpl(string(n.conf.APIKey))

	if err != nil {
		return nil, false, fmt.Errorf("templating error: %s", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, false, err
	}

	req, err := http.NewRequest("POST", apiURL.String(), &buf)
	if err != nil {
		return nil, true, err
	}
	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("Authorization", fmt.Sprintf("GenieKey %s", apiKey))
	return req, true, nil
}

func (n *OpsGenie) retry(statusCode int) (bool, error) {
	// https://docs.opsgenie.com/docs/response#section-response-codes
	// Response codes 429 (rate limiting) and 5xx are potentially recoverable
	if statusCode/100 == 5 || statusCode == 429 {
		return true, fmt.Errorf("unexpected status code %v", statusCode)
	} else if statusCode/100 != 2 {
		return false, fmt.Errorf("unexpected status code %v", statusCode)
	}

	return false, nil
}

// VictorOps implements a Notifier for VictorOps notifications.
type VictorOps struct {
	conf   *config.VictorOpsConfig
	tmpl   *template.Template
	logger log.Logger
}

// NewVictorOps returns a new VictorOps notifier.
func NewVictorOps(c *config.VictorOpsConfig, t *template.Template, l log.Logger) *VictorOps {
	return &VictorOps{
		conf:   c,
		tmpl:   t,
		logger: l,
	}
}

const (
	victorOpsEventTrigger = "CRITICAL"
	victorOpsEventResolve = "RECOVERY"
)

// Notify implements the Notifier interface.
func (n *VictorOps) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {

	var err error
	var (
		data   = n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)
		tmpl   = tmplText(n.tmpl, data, &err)
		apiURL = n.conf.APIURL.Copy()
	)
	apiURL.Path += fmt.Sprintf("%s/%s", n.conf.APIKey, tmpl(n.conf.RoutingKey))

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "victorops")
	if err != nil {
		return false, err
	}

	buf, err := n.createVictorOpsPayload(ctx, as...)
	if err != nil {
		return true, err
	}

	resp, err := post(ctx, c, apiURL.String(), contentTypeJSON, buf)
	if err != nil {
		return true, redactURL(err)
	}

	defer resp.Body.Close()

	return n.retry(resp.StatusCode)
}

// Create the JSON payload to be sent to the VictorOps API.
func (n *VictorOps) createVictorOpsPayload(ctx context.Context, as ...*types.Alert) (*bytes.Buffer, error) {
	victorOpsAllowedEvents := map[string]bool{
		"INFO":     true,
		"WARNING":  true,
		"CRITICAL": true,
	}

	key, ok := GroupKey(ctx)
	if !ok {
		return nil, fmt.Errorf("group key missing")
	}

	var err error
	var (
		alerts = types.Alerts(as...)
		data   = n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)
		tmpl   = tmplText(n.tmpl, data, &err)

		messageType  = tmpl(n.conf.MessageType)
		stateMessage = tmpl(n.conf.StateMessage)
	)

	if alerts.Status() == model.AlertFiring && !victorOpsAllowedEvents[messageType] {
		messageType = victorOpsEventTrigger
	}

	if alerts.Status() == model.AlertResolved {
		messageType = victorOpsEventResolve
	}

	stateMessage, truncated := truncate(stateMessage, 20480)
	if truncated {
		level.Debug(n.logger).Log("msg", "truncated stateMessage due to VictorOps stateMessage limit", "truncated_state_message", stateMessage, "incident", key)
	}

	msg := map[string]string{
		"message_type":        messageType,
		"entity_id":           hashKey(key),
		"entity_display_name": tmpl(n.conf.EntityDisplayName),
		"state_message":       stateMessage,
		"monitoring_tool":     tmpl(n.conf.MonitoringTool),
	}

	if err != nil {
		return nil, fmt.Errorf("templating error: %s", err)
	}

	// Add custom fields to the payload.
	for k, v := range n.conf.CustomFields {
		msg[k] = tmpl(v)
		if err != nil {
			return nil, fmt.Errorf("templating error: %s", err)
		}
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (n *VictorOps) retry(statusCode int) (bool, error) {
	// Missing documentation therefore assuming only 5xx response codes are
	// recoverable.
	if statusCode/100 == 5 {
		return true, fmt.Errorf("unexpected status code %v", statusCode)
	} else if statusCode/100 != 2 {
		return false, fmt.Errorf("unexpected status code %v", statusCode)
	}

	return false, nil
}

// Pushover implements a Notifier for Pushover notifications.
type Pushover struct {
	conf   *config.PushoverConfig
	tmpl   *template.Template
	logger log.Logger
	apiURL string // for tests.
}

// NewPushover returns a new Pushover notifier.
func NewPushover(c *config.PushoverConfig, t *template.Template, l log.Logger) *Pushover {
	return &Pushover{conf: c, tmpl: t, logger: l, apiURL: "https://api.pushover.net/1/messages.json"}
}

// Notify implements the Notifier interface.
func (n *Pushover) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	key, ok := GroupKey(ctx)
	if !ok {
		return false, fmt.Errorf("group key missing")
	}
	data := n.tmpl.Data(receiverName(ctx, n.logger), groupLabels(ctx, n.logger), as...)

	level.Debug(n.logger).Log("msg", "Notifying Pushover", "incident", key)

	var (
		err     error
		message string
	)
	tmpl := tmplText(n.tmpl, data, &err)
	tmplHTML := tmplHTML(n.tmpl, data, &err)

	parameters := url.Values{}
	parameters.Add("token", tmpl(string(n.conf.Token)))
	parameters.Add("user", tmpl(string(n.conf.UserKey)))

	title, truncated := truncate(tmpl(n.conf.Title), 250)
	if truncated {
		level.Debug(n.logger).Log("msg", "Truncated title due to Pushover title limit", "truncated_title", title, "incident", key)
	}
	parameters.Add("title", title)

	if n.conf.HTML {
		parameters.Add("html", "1")
		message = tmplHTML(n.conf.Message)
	} else {
		message = tmpl(n.conf.Message)
	}

	message, truncated = truncate(message, 1024)
	if truncated {
		level.Debug(n.logger).Log("msg", "Truncated message due to Pushover message limit", "truncated_message", message, "incident", key)
	}
	message = strings.TrimSpace(message)
	if message == "" {
		// Pushover rejects empty messages.
		message = "(no details)"
	}
	parameters.Add("message", message)

	supplementaryURL, truncated := truncate(tmpl(n.conf.URL), 512)
	if truncated {
		level.Debug(n.logger).Log("msg", "Truncated URL due to Pushover url limit", "truncated_url", supplementaryURL, "incident", key)
	}
	parameters.Add("url", supplementaryURL)
	parameters.Add("url_title", tmpl(n.conf.URLTitle))

	parameters.Add("priority", tmpl(n.conf.Priority))
	parameters.Add("retry", fmt.Sprintf("%d", int64(time.Duration(n.conf.Retry).Seconds())))
	parameters.Add("expire", fmt.Sprintf("%d", int64(time.Duration(n.conf.Expire).Seconds())))
	parameters.Add("sound", tmpl(n.conf.Sound))
	if err != nil {
		return false, err
	}

	u, err := url.Parse(n.apiURL)
	if err != nil {
		return false, err
	}
	u.RawQuery = parameters.Encode()
	// Don't log the URL as it contains secret data (see #1825).
	level.Debug(n.logger).Log("msg", "Sending Pushover message", "incident", key)

	c, err := commoncfg.NewClientFromConfig(*n.conf.HTTPConfig, "pushover")
	if err != nil {
		return false, err
	}

	resp, err := post(ctx, c, u.String(), "text/plain", nil)
	if err != nil {
		return true, redactURL(err)
	}
	defer resp.Body.Close()

	return n.retry(resp.StatusCode)
}

func (n *Pushover) retry(statusCode int) (bool, error) {
	// Only documented behaviour is that 2xx response codes are successful and
	// 4xx are unsuccessful, therefore assuming only 5xx are recoverable.
	// https://pushover.net/api#response
	if statusCode/100 == 5 {
		return true, fmt.Errorf("unexpected status code %v", statusCode)
	} else if statusCode/100 != 2 {
		return false, fmt.Errorf("unexpected status code %v", statusCode)
	}

	return false, nil
}

// tmplText is using monadic error handling in order to make string templating
// less verbose. Use with care as the final error checking is easily missed.
func tmplText(tmpl *template.Template, data *template.Data, err *error) func(string) string {
	return func(name string) (s string) {
		if *err != nil {
			return
		}
		s, *err = tmpl.ExecuteTextString(name, data)
		return s
	}
}

// tmplHTML is using monadic error handling in order to make string templating
// less verbose. Use with care as the final error checking is easily missed.
func tmplHTML(tmpl *template.Template, data *template.Data, err *error) func(string) string {
	return func(name string) (s string) {
		if *err != nil {
			return
		}
		s, *err = tmpl.ExecuteHTMLString(name, data)
		return s
	}
}

// hashKey returns the sha256 for a group key as integrations may have
// maximum length requirements on deduplication keys.
func hashKey(s string) string {
	h := sha256.New()
	// hash.Hash.Write never returns an error.
	//nolint: errcheck
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// redactURL removes the URL part from an error of *url.Error type.
func redactURL(err error) error {
	e, ok := err.(*url.Error)
	if !ok {
		return err
	}
	e.URL = "<redacted>"
	return e
}

func post(ctx context.Context, client *http.Client, url string, bodyType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", bodyType)
	return client.Do(req.WithContext(ctx))
}

func truncate(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	if n <= 3 {
		return string(r[:n]), true
	}
	return string(r[:n-3]) + "...", true
}
