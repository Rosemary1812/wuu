package toolctx

import "context"

type stepIndexKey struct{}

// WithStepIndex annotates tool execution context with the model step that
// requested the tool. The value is telemetry-only; tools must not branch on it.
func WithStepIndex(ctx context.Context, stepIndex int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, stepIndexKey{}, stepIndex)
}

func StepIndex(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	stepIndex, ok := ctx.Value(stepIndexKey{}).(int)
	return stepIndex, ok
}
