package providers

import (
	"encoding/json"
	"math/big"
	"strings"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
)

type vendorResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        uint64 `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string  `json:"role"`
			Content string  `json:"content"`
			Refusal *string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		Prompt        *uint64 `json:"prompt_tokens"`
		Completion    *uint64 `json:"completion_tokens"`
		Total         *uint64 `json:"total_tokens"`
		PromptDetails struct {
			Cached uint64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func parseResponse(body []byte, enc brain.Encoding, cfg OpenAIConfig, p brain.Profile, status int) (brain.Generated, TokenUsage, string, error) {
	unknown := api.E("accounting_unknown", "model_original_usage_unknown")
	if _, err := api.ParseJSON(body); err != nil {
		return brain.Generated{}, TokenUsage{}, "", unknown
	}
	var reply vendorResponse
	// 外部 envelope 可增加供应商元数据；只投影实际需要的固定字段。
	// 内层模型输出仍必须经过闭合 Schema，未知字段不能进入公开 Proposal。
	if err := json.Unmarshal(body, &reply); err != nil {
		return brain.Generated{}, TokenUsage{}, "", unknown
	}
	if reply.Usage == nil || reply.Usage.Prompt == nil || reply.Usage.Completion == nil || reply.Usage.Total == nil {
		return brain.Generated{}, TokenUsage{}, reply.ID, unknown
	}
	u := TokenUsage{Prompt: *reply.Usage.Prompt, Completion: *reply.Usage.Completion, Total: *reply.Usage.Total, CachedInput: reply.Usage.PromptDetails.Cached}
	if u.Prompt > api.MaxSafeInteger || u.Completion > api.MaxSafeInteger-u.Prompt || u.Total != u.Prompt+u.Completion || u.CachedInput > u.Prompt {
		return brain.Generated{}, u, reply.ID, unknown
	}
	usd, err := priceUSD(u, cfg)
	if err != nil {
		return brain.Generated{}, u, reply.ID, unknown
	}
	usage := []api.Amount{{Unit: "USD", Value: usd}}
	invalid := brain.Generated{Contents: []brain.GeneratedContent{}, Usage: usage, UsageFinal: cfg.BillingFinal}
	var wire wireRequest
	var input userInput
	if err = api.Decode(enc.Body, &wire); err != nil || len(wire.Messages) != 2 {
		return invalid, u, reply.ID, nil
	}
	if err = api.Decode([]byte(wire.Messages[1].Content), &input); err != nil {
		return invalid, u, reply.ID, nil
	}
	if status != 200 || reply.ID == "" || len(reply.ID) > 512 || len(reply.Choices) != 1 || reply.Choices[0].Index != 0 || reply.Choices[0].FinishReason != "stop" || reply.Choices[0].Message.Role != "assistant" || reply.Choices[0].Message.Refusal != nil && *reply.Choices[0].Message.Refusal != "" || u.Prompt > enc.InputTokens || u.Completion > p.MaxOutputTokens || u.Completion > wire.MaxCompletionTokens {
		return invalid, u, reply.ID, nil
	}
	out, err := ParseGenerated([]byte(reply.Choices[0].Message.Content))
	if err != nil {
		return invalid, u, reply.ID, nil
	}
	for _, content := range out.Contents {
		for _, disclosed := range content.DisclosedSources {
			found := false
			for _, processed := range enc.ProcessedSources {
				if disclosed == processed {
					found = true
					break
				}
			}
			if !found {
				return invalid, u, reply.ID, nil
			}
		}
	}
	for _, action := range out.Draft.Actions {
		capability, binding := false, false
		for _, ref := range input.Snapshot.CapabilityRefs {
			if ref == action.CapabilityRef {
				capability = true
				break
			}
		}
		for _, ref := range input.Snapshot.BindingRefs {
			if ref == action.BindingRef {
				binding = true
				break
			}
		}
		if !capability || !binding {
			return invalid, u, reply.ID, nil
		}
	}
	for _, ref := range out.Draft.ExistingArtifactRefs {
		found := false
		for _, source := range enc.ProcessedSources {
			if ref == source {
				found = true
				break
			}
		}
		if !found {
			return invalid, u, reply.ID, nil
		}
	}
	out.Usage = usage
	out.UsageFinal = cfg.BillingFinal
	return out, u, reply.ID, nil
}

// USD 合同按百万 token 费率求和，再向上取整到 9 位小数；不使用浮点数。
func priceUSD(u TokenUsage, c OpenAIConfig) (string, error) {
	return priceAtPrecision(u, c, 9)
}

// CostBound 按准确输入计数与预留输出计算 USD 上界；无缓存优惠假设，
// 若缓存单价更高则采用该单价，最后向上取整到 6 位小数。
func (e *OpenAI) CostBound(inputTokens, reservedOutputTokens uint64) ([]api.Amount, error) {
	if inputTokens > e.profile.MaxInputTokens || reservedOutputTokens > e.profile.MaxOutputTokens || reservedOutputTokens > e.profile.ContextLimit-e.profile.SafetyMargin || inputTokens > e.profile.ContextLimit-e.profile.SafetyMargin-reservedOutputTokens {
		return nil, api.E("invalid_request", "model_cost_bound_over_profile")
	}
	cfg := e.cfg
	comparison, err := api.CompareDecimal(cfg.CachedInputUSDPerMillion, cfg.InputUSDPerMillion)
	if err != nil {
		return nil, err
	}
	if comparison > 0 {
		cfg.InputUSDPerMillion = cfg.CachedInputUSDPerMillion
	}
	usd, err := priceAtPrecision(TokenUsage{Prompt: inputTokens, Completion: reservedOutputTokens}, cfg, 6)
	if err != nil {
		return nil, err
	}
	return []api.Amount{{Unit: "USD", Value: usd}}, nil
}

func priceAtPrecision(u TokenUsage, c OpenAIConfig, places int) (string, error) {
	total := new(big.Int)
	for _, term := range []struct {
		tokens uint64
		rate   string
	}{{u.Prompt - u.CachedInput, c.InputUSDPerMillion}, {u.CachedInput, c.CachedInputUSDPerMillion}, {u.Completion, c.OutputUSDPerMillion}} {
		if _, err := api.CompareDecimal(term.rate, "0"); err != nil {
			return "", err
		}
		whole, fraction, _ := strings.Cut(term.rate, ".")
		rate, ok := new(big.Int).SetString(whole+fraction+strings.Repeat("0", 9-len(fraction)), 10)
		if !ok {
			return "", api.E("invalid_request", "model_tariff_invalid")
		}
		total.Add(total, new(big.Int).Mul(rate, new(big.Int).SetUint64(term.tokens)))
	}
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(15-places)), nil)
	total.Add(total, new(big.Int).Sub(unit, big.NewInt(1)))
	total.Div(total, unit)
	digits := total.String()
	if len(digits) <= places {
		digits = strings.Repeat("0", places+1-len(digits)) + digits
	}
	whole, fraction := digits[:len(digits)-places], strings.TrimRight(digits[len(digits)-places:], "0")
	if fraction == "" {
		return whole, nil
	}
	return whole + "." + fraction, nil
}
