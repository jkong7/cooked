package ai

import (
	"context"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

const DefaultModel = "claude-opus-5"

type Claude struct {
	client   anthropic.Client
	Model    string
	Fallback Brain
	Timeout  time.Duration
}

func NewClaude(apiKey, model string, extra ...option.RequestOption) *Claude {
	if model == "" {
		model = DefaultModel
	}
	opts := append([]option.RequestOption{option.WithMaxRetries(1)}, extra...)
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &Claude{client: anthropic.NewClient(opts...), Model: model, Fallback: Local{}, Timeout: 25 * time.Second}
}

const judgeSystem = `You judge "cooked", a live 1v1 debate game where friends argue hot takes for fun.
Pick a winner on persuasiveness, wit, and actually answering the other side. Missing a turn is a big loss.
Ignore which side you personally agree with. Humor is welcome; cruelty about protected traits, bodies, or real harm is not.
Roasts target how someone argued, never who they are.
Reply with only a JSON object:
{"winner":"a" or "b","score_a":0-100 (A's share of the win),"headline":"under 12 words, e.g. 'Priya cooked Marcus'","reason":"one sentence","roast_a":"one short line about A's debating","roast_b":"one short line about B's debating"}`

const argueSystem = `You are a debate opponent in "cooked", a live 1v1 hot-take game. Argue your assigned side hard and with humor, in the voice of a sharp, funny 22-year-old.
Rules: one or two sentences, under 240 characters, no hashtags, no emojis, no slurs, nothing about protected traits or anyone's body. Respond directly to the other side's last point when there is one.
Reply with only the argument text.`

const modSystem = `You moderate a debate game for adults. Block only: slurs, hate toward protected groups, threats, encouraging self-harm, sexual content involving minors, or personal info like phone numbers and addresses. Allow trash talk, profanity, and spicy opinions.
Reply with only a JSON object: {"allow": true or false, "reason": "short reason if blocked"}`

func (c *Claude) ask(ctx context.Context, system, user string, maxTokens int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	resp, err := c.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     c.Model,
		MaxTokens: maxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user))},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
	})
	if err != nil {
		return "", err
	}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return "", ErrBadJSON
	}
	var out strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			out.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func (c *Claude) Moderate(ctx context.Context, text string) (bool, string) {
	if ok, why := c.Fallback.Moderate(ctx, text); !ok {
		return false, why
	}
	mctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	raw, err := c.ask(mctx, modSystem, "Message:\n"+text, 2000)
	if err != nil {
		return true, ""
	}
	var r struct {
		Allow  bool   `json:"allow"`
		Reason string `json:"reason"`
	}
	if extractJSON(raw, &r) != nil || r.Allow {
		return true, ""
	}
	return false, clip(r.Reason, 80)
}

func (c *Claude) Argue(ctx context.Context, d Debate, side string) (string, error) {
	user := d.Transcript() + "\nYou are " + strings.ToUpper(side) + " arguing " + SideLabel(side) + ". Write your next turn."
	raw, err := c.ask(ctx, argueSystem, user, 4000)
	if err != nil || raw == "" {
		return c.Fallback.Argue(ctx, d, side)
	}
	raw = strings.Trim(raw, "\"")
	if ok, _ := c.Fallback.Moderate(ctx, raw); !ok {
		return c.Fallback.Argue(ctx, d, side)
	}
	return clip(raw, 280), nil
}

func (c *Claude) Judge(ctx context.Context, d Debate) (Verdict, error) {
	raw, err := c.ask(ctx, judgeSystem, d.Transcript(), 8000)
	if err != nil {
		return c.Fallback.Judge(ctx, d)
	}
	var v Verdict
	if extractJSON(raw, &v) != nil {
		return c.Fallback.Judge(ctx, d)
	}
	v.normalize(d)
	return v, nil
}
