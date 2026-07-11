package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type fakeClient struct {
	id       string
	hooks    []Hook
	status   Status
	invoke   func(InvokeParams) (InvokeResult, error)
	closed   bool
	closeLog *[]string
}

func (f *fakeClient) ID() string     { return f.id }
func (f *fakeClient) Hooks() []Hook  { return append([]Hook(nil), f.hooks...) }
func (f *fakeClient) Status() Status { return f.status }
func (f *fakeClient) Invoke(_ context.Context, p InvokeParams) (InvokeResult, error) {
	return f.invoke(p)
}
func (f *fakeClient) Close(context.Context) error {
	f.closed = true
	if f.closeLog != nil {
		*f.closeLog = append(*f.closeLog, f.id)
	}
	return nil
}

func TestHostRunChainsTypedOutputInDiscoveryOrder(t *testing.T) {
	type output struct {
		Value string `json:"value"`
	}
	client := func(id, suffix string) *fakeClient {
		return &fakeClient{id: id, hooks: []Hook{HookChatMessage}, invoke: func(params InvokeParams) (InvokeResult, error) {
			var current output
			if err := json.Unmarshal(params.Output, &current); err != nil {
				t.Fatal(err)
			}
			current.Value += suffix
			data, _ := json.Marshal(current)
			return InvokeResult{Output: data}, nil
		}}
	}
	host := New(client("one", "-one"), client("two", "-two"))
	got := output{Value: "start"}
	if err := host.Run(context.Background(), HookChatMessage, map[string]string{"session_id": "s"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Value != "start-one-two" {
		t.Fatalf("output = %q", got.Value)
	}
}

func TestHostRunStopsOnPluginFailure(t *testing.T) {
	secondCalled := false
	host := New(
		&fakeClient{id: "broken", hooks: []Hook{HookToolExecuteBefore}, invoke: func(InvokeParams) (InvokeResult, error) {
			return InvokeResult{}, errors.New("boom")
		}},
		&fakeClient{id: "second", hooks: []Hook{HookToolExecuteBefore}, invoke: func(params InvokeParams) (InvokeResult, error) {
			secondCalled = true
			return InvokeResult{Output: params.Output}, nil
		}},
	)
	out := map[string]any{"args": map[string]any{}}
	err := host.Run(context.Background(), HookToolExecuteBefore, nil, &out)
	if err == nil || secondCalled {
		t.Fatalf("err = %v, secondCalled = %v", err, secondCalled)
	}
}

func TestHostCloseUsesReverseInitializationOrder(t *testing.T) {
	var closed []string
	host := New(
		&fakeClient{id: "one", closeLog: &closed},
		&fakeClient{id: "two", closeLog: &closed},
	)
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(closed, []string{"two", "one"}) {
		t.Fatalf("closed = %v", closed)
	}
}

func TestHostRejectsUnknownHook(t *testing.T) {
	out := struct{}{}
	if err := New().Run(context.Background(), Hook("unknown"), nil, &out); err == nil {
		t.Fatal("expected unknown hook error")
	}
}
