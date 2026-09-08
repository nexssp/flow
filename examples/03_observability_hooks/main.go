package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type traceCtxKey string

const startTimeKey traceCtxKey = "start_time"

func TelemetryHook() action.AnyHook {
	return action.AnyHook{
		Before: func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			fmt.Printf("[TRACE] 🟢 START  [%s] input=%v\n", meta.Name, req)
			return context.WithValue(ctx, startTimeKey, time.Now()), nil
		},
		After: func(ctx context.Context, req any, res any, err error, meta *action.Meta) {
			start, _ := ctx.Value(startTimeKey).(time.Time)
			if err != nil {
				fmt.Printf("[TRACE] 🔴 ERROR  [%s] duration=%v err=%v\n", meta.Name, time.Since(start), err)
				return
			}
			fmt.Printf("[TRACE] 🏁 FINISH [%s] duration=%v output=%v\n", meta.Name, time.Since(start), res)
		},
	}
}

func main() {
	ctx := context.Background()

	dbQuery := action.New("db.query", func(_ context.Context, _ int) (map[string]any, error) {
		time.Sleep(30 * time.Millisecond)
		return map[string]any{
			"user_email": "alice@nexss.com",
			"host":       "edge-03",
			"cpu_pct":    97,
		}, nil
	}).Build()

	emailAlert := action.New("email.alert", func(_ context.Context, req map[string]any) (string, error) {
		return fmt.Sprintf("Alert email sent to %s", req["to"]), nil
	}).Build()

	registry := flow.NewRegistry(dbQuery, emailAlert)

	for _, act := range registry.Actions() {
		act.AddAnyHook(TelemetryHook())
	}

	dsl := `db.query -> { to: user_email, subject: "CPU alert on " + host } -> email.alert`

	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	res, err := builder.Build().Do(ctx, 42)
	if err != nil {
		panic(err)
	}

	fmt.Printf("\n✅ Final pipeline output: %v\n", res)
}
