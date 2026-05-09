package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/operator"
)

const WithRepairDescription = `WithRepair: AI-driven recovery wrapper around a deterministic op.
  Mechanism: When the wrapped op returns *library.ErrRepairable, the wrapper
             forwards the error's Prompt verbatim (sandwiched by a configured
             PromptPrefix/PromptSuffix) to the LLM, parses the response into a
             fresh value of the named input field's type via the type's
             UnmarshalRepair method, writes it back via inner.SetInputField,
             and re-runs the inner op. Up to MaxAttempts repair cycles per Run;
             non-repairable errors are propagated unchanged.
  Inner contract:
             - Inner op returns *library.ErrRepairable when the failure is
               structural and fixable by an LLM mutation of its input.
             - The named input field must be a pointer type (e.g. *MyInput)
               whose element type implements library.RepairableInput
               (UnmarshalRepair(string) error).
             - Inner op MUST be idempotent or pure — repair retries re-execute Run.
  Registration:
             library.RegisterWithRepair[*InnerOp](name, factory, RepairConfig{...})
             called from init(). The wrapped vertex exposes the inner op's
             input/output field names verbatim — wire it in buildGraph
             identically to the unwrapped inner op.
  Params:    max_attempts string — repair cycle budget (default "3").
             provider     string — AI provider: "claude" (default) or "gemini".
             model        string — model passed to the provider (default "claude-sonnet-4-6").
             max_tokens   string — LLM response token budget (default "2048").
             api_retries        — see provider-level retry settings.
  Inputs/Outputs: identical to the wrapped inner op — wire by the inner op's field names.`

// ErrRepairable is the typed error inner ops return when their failure may be
// fixed by an LLM-driven minimal-mutation retry of their input. The Prompt
// field carries a complete, self-contained instruction for the LLM that, when
// fulfilled, produces text the input type can deserialize via UnmarshalRepair.
//
// Inner ops construct *ErrRepairable themselves; the WithRepair wrapper
// extracts Prompt verbatim (sandwiched by RepairConfig.PromptPrefix/Suffix)
// and forwards it to the LLM. Use errors.As(err, &target) to detect.
type ErrRepairable struct {
	Prompt string // self-contained instruction for the LLM
	Cause  error  // underlying validation/parse error (logged; participates in errors.Unwrap)
}

func (e *ErrRepairable) Error() string {
	if e == nil {
		return "repairable: <nil>"
	}
	if e.Cause != nil {
		return "repairable: " + e.Cause.Error()
	}
	return "repairable"
}

func (e *ErrRepairable) Unwrap() error { return e.Cause }

// RepairableInput is implemented by the input type the wrapper repairs. The
// wrapper allocates a fresh zero value of the type, calls UnmarshalRepair on
// the LLM's response, and writes the resulting pointer back into the inner op
// via SetInputField.
//
// Implementations may delegate to xml.Unmarshal, json.Unmarshal, strconv
// parsing, or any other strategy — whatever pairs with the prompt the op
// emits.
type RepairableInput interface {
	UnmarshalRepair(response string) error
}

// RepairConfig describes how to wrap one op for AI-driven repair.
type RepairConfig struct {
	// InputField is required: the name of the input field on Inner whose value
	// the LLM will repair. The field's element type (T, where the field type
	// is *T) must implement RepairableInput.
	InputField string

	// MaxAttempts caps the number of repair cycles per Run. Default 3 when 0.
	MaxAttempts int

	// PromptPrefix is prepended to ErrRepairable.Prompt verbatim.
	PromptPrefix string

	// PromptSuffix is appended to ErrRepairable.Prompt verbatim.
	PromptSuffix string

	// Provider is the AI provider ("claude" or "gemini"). Default "claude".
	Provider string

	// Model is passed through to the provider SDK. Default "claude-sonnet-4-6".
	Model string

	// MaxTokens is the response token budget for the repair LLM call. Default 2048.
	MaxTokens int64
}

// withRepairOp is the runtime wrapper. It implements operator.IOperator and
// proxies all field-routing methods to the inner op.
type withRepairOp[Inner operator.IOperator] struct {
	inner  Inner
	config RepairConfig
	caller aiCaller
	name   string // registered name, for error messages
}

// RegisterWithRepair registers a repair-wrapped op under `name` in dagor's
// global operator registry. From buildGraph's perspective the wrapped vertex
// is wired identically to the inner op (same input/output field names).
//
// Inner is constrained to operator.IOperator, so any registered dagor op type
// can be wrapped. The factory closure constructs a fresh inner instance per
// op-pool acquire; the wrapper holds it by value.
//
// Returns the underlying RegisterOpFactory error for symmetry with the rest
// of the registry; callers typically log.Fatal it inside init().
func RegisterWithRepair[Inner operator.IOperator](
	name string,
	factory func() Inner,
	cfg RepairConfig,
) error {
	if name == "" {
		return errors.New("RegisterWithRepair: name is required")
	}
	if factory == nil {
		return errors.New("RegisterWithRepair: factory is required")
	}
	if cfg.InputField == "" {
		return errors.New("RegisterWithRepair: RepairConfig.InputField is required")
	}
	return operator.RegisterOpFactory(name, func() operator.IOperator {
		return &withRepairOp[Inner]{
			inner:  factory(),
			config: cfg,
			name:   name,
		}
	})
}

func (op *withRepairOp[Inner]) Setup(params *config.Params) error {
	if err := op.inner.Setup(params); err != nil {
		return fmt.Errorf("WithRepair[%s]: inner Setup: %w", op.name, err)
	}

	// Apply RepairConfig defaults.
	if op.config.MaxAttempts <= 0 {
		op.config.MaxAttempts = 3
	}
	if op.config.Provider == "" {
		op.config.Provider = "claude"
	}
	if op.config.Model == "" {
		op.config.Model = "claude-sonnet-4-6"
	}
	if op.config.MaxTokens <= 0 {
		op.config.MaxTokens = 2048
	}

	// Vertex-level params override registration defaults — lets graph authors
	// dial budgets without re-registering.
	op.config.MaxAttempts = params.GetInt("max_attempts", op.config.MaxAttempts)
	op.config.Provider = params.GetString("provider", op.config.Provider)
	op.config.Model = params.GetString("model", op.config.Model)
	op.config.MaxTokens = params.GetInt64("max_tokens", op.config.MaxTokens)

	if err := validateRepairableField(op.inner, op.config.InputField); err != nil {
		return fmt.Errorf("WithRepair[%s]: %w", op.name, err)
	}

	caller, err := newAICaller(op.config.Provider, op.config.Model, parseRetryConfig(params))
	if err != nil {
		return fmt.Errorf("WithRepair[%s]: %w", op.name, err)
	}
	op.caller = caller
	return nil
}

func (op *withRepairOp[Inner]) Reset() error                                   { return op.inner.Reset() }
func (op *withRepairOp[Inner]) ResetFields()                                   { op.inner.ResetFields() }
func (op *withRepairOp[Inner]) InputFields() map[string]any                    { return op.inner.InputFields() }
func (op *withRepairOp[Inner]) OutputFields() map[string]any                   { return op.inner.OutputFields() }
func (op *withRepairOp[Inner]) SetInputField(field string, value any) error    { return op.inner.SetInputField(field, value) }

func (op *withRepairOp[Inner]) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "WithRepair.run", "run_id", dagor.RunID(ctx), "name", op.name, "input_field", op.config.InputField)

	err := op.inner.Run(ctx)
	if err == nil {
		return nil
	}

	var rep *ErrRepairable
	if !errors.As(err, &rep) {
		return err
	}

	for attempt := 1; attempt <= op.config.MaxAttempts; attempt++ {
		fullPrompt := op.config.PromptPrefix + rep.Prompt + op.config.PromptSuffix
		res, callErr := op.caller.call(ctx, aiCallRequest{
			SystemText: "You are a strict data-repair assistant. Output exactly what the user asks for, with no prose, no commentary, and no markdown fences.",
			Prompt:     fullPrompt,
			MaxTokens:  op.config.MaxTokens,
		})
		if callErr != nil {
			return fmt.Errorf("WithRepair[%s] attempt %d: LLM call: %w", op.name, attempt, callErr)
		}
		slog.InfoContext(ctx, "WithRepair.tokens", "run_id", dagor.RunID(ctx), "name", op.name, "attempt", attempt, "input_tokens", res.InputTokens, "output_tokens", res.OutputTokens)

		newVal, parseErr := allocAndUnmarshal(op.inner, op.config.InputField, res.Text)
		if parseErr != nil {
			slog.DebugContext(ctx, "WithRepair.unparseable", "run_id", dagor.RunID(ctx), "name", op.name, "attempt", attempt, "error", parseErr)
			rep = &ErrRepairable{
				Prompt: rep.Prompt + "\n\nYour previous response was unparseable: " + parseErr.Error() + ". Try again.",
				Cause:  parseErr,
			}
			continue
		}

		if setErr := op.inner.SetInputField(op.config.InputField, newVal); setErr != nil {
			return fmt.Errorf("WithRepair[%s] attempt %d: SetInputField %q: %w", op.name, attempt, op.config.InputField, setErr)
		}

		err = op.inner.Run(ctx)
		if err == nil {
			recordReasoning(ctx, "WithRepair", map[string]any{
				"name":         op.name,
				"input_field":  op.config.InputField,
				"max_attempts": op.config.MaxAttempts,
			}, op.inner.OutputFields(), fmt.Sprintf("repaired after %d attempt(s)", attempt))
			return nil
		}
		if !errors.As(err, &rep) {
			return err
		}
	}

	return fmt.Errorf("WithRepair[%s]: %d repair attempt(s) exhausted: %w", op.name, op.config.MaxAttempts, err)
}

// validateRepairableField is called once at Setup so misconfiguration fails
// fast at graph instantiation rather than at Run-time.
func validateRepairableField(inner operator.IOperator, fieldName string) error {
	fields := inner.InputFields()
	fp, ok := fields[fieldName]
	if !ok {
		return fmt.Errorf("input field %q not present in inner.InputFields() (have: %v)", fieldName, fieldNames(fields))
	}
	fpType := reflect.TypeOf(fp)
	if fpType == nil || fpType.Kind() != reflect.Ptr {
		return fmt.Errorf("input field %q: expected **T from InputFields(), got %v", fieldName, fpType)
	}
	pType := fpType.Elem() // *T
	if pType.Kind() != reflect.Ptr {
		return fmt.Errorf("input field %q: must be a pointer (got element kind %s)", fieldName, pType.Kind())
	}
	elemType := pType.Elem() // T
	sample := reflect.New(elemType).Interface()
	if _, ok := sample.(RepairableInput); !ok {
		return fmt.Errorf("input field %q (type *%s) does not implement library.RepairableInput", fieldName, elemType.Name())
	}
	return nil
}

// allocAndUnmarshal allocates a fresh value of the inner op's named input
// field's element type, asks RepairableInput to unmarshal `text` into it,
// and returns the new pointer (typed for SetInputField).
func allocAndUnmarshal(inner operator.IOperator, fieldName, text string) (any, error) {
	fields := inner.InputFields()
	fp, ok := fields[fieldName]
	if !ok {
		return nil, fmt.Errorf("input field %q not present in inner.InputFields()", fieldName)
	}
	fpType := reflect.TypeOf(fp)
	if fpType == nil || fpType.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("input field %q: expected **T, got %v", fieldName, fpType)
	}
	pType := fpType.Elem()
	if pType.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("input field %q: must be a pointer, got %s", fieldName, pType.Kind())
	}
	elemType := pType.Elem()
	newPtr := reflect.New(elemType)
	target, ok := newPtr.Interface().(RepairableInput)
	if !ok {
		return nil, fmt.Errorf("input field %q (type *%s) does not implement RepairableInput", fieldName, elemType.Name())
	}
	if err := target.UnmarshalRepair(text); err != nil {
		return nil, fmt.Errorf("UnmarshalRepair: %w", err)
	}
	return newPtr.Interface(), nil
}

func fieldNames(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
