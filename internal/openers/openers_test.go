package openers

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	configv1 "github.com/solodov/recall/proto/recall/config/v1"
)

func TestParseRecallURLDecodesFileTarget(t *testing.T) {
	target, err := ParseRecallURL("recall://open?v=1&source=code&selector=file%3Acontent&type=file&path=%2Fworkspace%2Fmain.kt&line=12&column=4")
	if err != nil {
		t.Fatalf("ParseRecallURL returned error: %v", err)
	}
	if target.Source != "code" || target.Selector != "file:content" || target.Type != TargetTypeFile || target.Path != "/workspace/main.kt" {
		t.Fatalf("target metadata = %#v", target)
	}
	if !target.HasLine || target.Line != 12 || !target.HasColumn || target.Column != 4 {
		t.Fatalf("target location = %#v", target)
	}
}

func TestOpenPrefersSpecificOpenerOverGenericDefault(t *testing.T) {
	cfg := &configv1.RecallConfig{Openers: []*configv1.Opener{
		{
			Id:          "file-default",
			TargetTypes: []string{TargetTypeFile},
			Command:     "default-editor",
			Args:        []string{"+{line}:{column}", "{path}"},
		},
		{
			Id:          "web",
			TargetTypes: []string{TargetTypeURI},
			Command:     "open",
			Args:        []string{"{uri}"},
		},
		{
			Id:          "code",
			Sources:     []string{"code"},
			Selectors:   []string{"file:content"},
			TargetTypes: []string{TargetTypeFile},
			Command:     "editor",
			Args:        []string{"+call cursor({line}, {column})", "{path}"},
		},
	}}
	runner := &recordingRunner{}

	err := Open(context.Background(), cfg, "recall://open?v=1&source=code&selector=file%3Acontent&type=file&path=%2Fworkspace%2Fmain.kt&line=12&column=4", Options{Runner: runner.Run})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "editor" || !reflect.DeepEqual(runner.args, []string{"+call cursor(12, 4)", "/workspace/main.kt"}) {
		t.Fatalf("runner = %q %#v", runner.command, runner.args)
	}
}

func TestOpenUsesGenericFileOpenerAsDefault(t *testing.T) {
	cfg := &configv1.RecallConfig{Openers: []*configv1.Opener{{
		Id:          "file-default",
		TargetTypes: []string{TargetTypeFile},
		Command:     "editor",
		Args:        []string{"--no-wait", "+{line}:{column}", "{path}"},
	}}}
	runner := &recordingRunner{}

	err := Open(context.Background(), cfg, "recall://open?v=1&source=org&selector=entry%3Acontent&type=file&path=%2Fworkspace%2Fconfig.txtpb&line=14&column=1", Options{Runner: runner.Run})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "editor" || !reflect.DeepEqual(runner.args, []string{"--no-wait", "+14:1", "/workspace/config.txtpb"}) {
		t.Fatalf("runner = %q %#v, want generic file opener", runner.command, runner.args)
	}
}

func TestOpenExpandsURITimestampPlaceholder(t *testing.T) {
	cfg := &configv1.RecallConfig{Openers: []*configv1.Opener{{
		Id:          "slack",
		TargetTypes: []string{TargetTypeURI},
		Command:     "open-message",
		Args:        []string{"{uri}", "{timestamp}"},
	}}}
	runner := &recordingRunner{}

	err := Open(context.Background(), cfg, "recall://open?v=1&type=uri&uri=https%3A%2F%2Fexample.invalid%2Fmessage&timestamp=2026-04-29T10%3A15%3A30.123456Z", Options{Runner: runner.Run})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "open-message" || !reflect.DeepEqual(runner.args, []string{"https://example.invalid/message", "2026-04-29T10:15:30.123456Z"}) {
		t.Fatalf("runner = %q %#v", runner.command, runner.args)
	}
}

func TestOpenFallsBackWhenNoConfiguredOpenerMatches(t *testing.T) {
	runner := &recordingRunner{}

	err := Open(context.Background(), &configv1.RecallConfig{}, "recall://open?v=1&type=uri&uri=https%3A%2F%2Fexample.invalid%2Fdoc", Options{Runner: runner.Run, FallbackCommand: "fallback-open"})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "fallback-open" || !reflect.DeepEqual(runner.args, []string{"https://example.invalid/doc"}) {
		t.Fatalf("fallback runner = %q %#v", runner.command, runner.args)
	}
}

func TestOpenFallsBackWithExactOriginalURI(t *testing.T) {
	runner := &recordingRunner{}
	rawURI := "org-protocol:/roam-node?node=89808715-6315-4484-B726-DFC9F4F2345D"

	err := Open(context.Background(), &configv1.RecallConfig{}, "recall://open?v=1&type=uri&uri=org-protocol%3A%2Froam-node%3Fnode%3D89808715-6315-4484-B726-DFC9F4F2345D", Options{Runner: runner.Run, FallbackCommand: "fallback-open"})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "fallback-open" || !reflect.DeepEqual(runner.args, []string{rawURI}) {
		t.Fatalf("fallback runner = %q %#v", runner.command, runner.args)
	}
}

func TestOpenSkipsOpenerWithMissingPlaceholder(t *testing.T) {
	cfg := &configv1.RecallConfig{Openers: []*configv1.Opener{{
		Id:          "needs-line",
		TargetTypes: []string{TargetTypeFile},
		Command:     "editor",
		Args:        []string{"+{line}", "{path}"},
	}}}
	runner := &recordingRunner{}

	err := Open(context.Background(), cfg, "recall://open?v=1&type=file&path=%2Fworkspace%2Fmain.kt", Options{Runner: runner.Run, FallbackCommand: "fallback-open"})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "fallback-open" || !reflect.DeepEqual(runner.args, []string{"/workspace/main.kt"}) {
		t.Fatalf("fallback runner = %q %#v", runner.command, runner.args)
	}
}

func TestOpenFallsThroughToPlainFileOpenerWhenLocationIsMissing(t *testing.T) {
	cfg := &configv1.RecallConfig{Openers: []*configv1.Opener{
		{
			Id:          "file-location",
			TargetTypes: []string{TargetTypeFile},
			Command:     "editor",
			Args:        []string{"+{line}:{column}", "{path}"},
		},
		{
			Id:          "file-default",
			TargetTypes: []string{TargetTypeFile},
			Command:     "editor",
			Args:        []string{"{path}"},
		},
	}}
	runner := &recordingRunner{}

	err := Open(context.Background(), cfg, "recall://open?v=1&type=file&path=%2Fworkspace%2Fmain.kt", Options{Runner: runner.Run, FallbackCommand: "fallback-open"})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if runner.command != "editor" || !reflect.DeepEqual(runner.args, []string{"/workspace/main.kt"}) {
		t.Fatalf("runner = %q %#v, want plain file opener", runner.command, runner.args)
	}
}

func TestOpenLogsDispatch(t *testing.T) {
	var logs bytes.Buffer
	runner := &recordingRunner{}
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	recallURL := "recall://open?v=1&type=uri&uri=org-protocol%3A%2Froam-node%3Fnode%3D89808715-6315-4484-B726-DFC9F4F2345D"

	err := Open(context.Background(), &configv1.RecallConfig{}, recallURL, Options{Runner: runner.Run, FallbackCommand: "fallback-open", Logger: logger})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	logText := logs.String()
	for _, want := range []string{
		"recall-open dispatch",
		"recall_url=",
		"target_type=uri",
		"target_uri=\"org-protocol:/roam-node?node=89808715-6315-4484-B726-DFC9F4F2345D\"",
		"uri_scheme=org-protocol",
		"command=fallback-open",
		"fallback=true",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("dispatch log %q does not contain %q", logText, want)
		}
	}
}

type recordingRunner struct {
	command string
	args    []string
}

func (runner *recordingRunner) Run(_ context.Context, command string, args []string) error {
	runner.command = command
	runner.args = append([]string{}, args...)
	return nil
}
