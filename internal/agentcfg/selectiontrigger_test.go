package agentcfg

import (
	"reflect"
	"testing"
)

// selectiontrigger_test.go pins the SELECTION TRIGGER (SelectionTriggerKey; a term coined in
// selection.go for docs/design/model-lists-and-pickers.md MM-D43): a derive that emits one says
// that a change of the trigger, and only that, makes its selection a new choice. claude's
// settings derive emits its provider there, so its saved `model` is written once per provider
// change and a user's /model pick on the same provider stands, even when the value the derive
// computes moves with the file it reads.

func TestApplySelectionTrigger(t *testing.T) {
	const trig = SelectionTriggerKey
	cases := []struct {
		name      string
		selection map[string]any
		file      map[string]any
		record    map[string]any
		wantLift  map[string]any
		wantNext  map[string]any
		wantClear []string
	}{
		{
			name:      "the first render writes the value and records the trigger",
			selection: map[string]any{trig: "zai", "model": "glm-5.3"},
			file:      map[string]any{},
			wantLift:  map[string]any{"model": "glm-5.3"},
			wantNext:  map[string]any{trig: "zai", "model": "glm-5.3"},
		},
		{
			name:      "a trigger alone records the trigger and writes nothing",
			selection: map[string]any{trig: ""},
			file:      map[string]any{"model": "claude-opus-5-5"},
			wantLift:  map[string]any{},
			wantNext:  map[string]any{trig: ""},
		},
		{
			// The value moved because the derive computed it from the user's own pick; the
			// trigger did not move, so the pick stands. Without a trigger the old rule
			// ("a new value outranks a stale edit") would write over it.
			name:      "a user edit stands while the trigger is unchanged, whatever the value",
			selection: map[string]any{trig: "zai", "model": "glm-5.3"},
			file:      map[string]any{"model": "glm-4.6"},
			record:    map[string]any{trig: "zai", "model": "glm-5.3-flash"},
			wantLift:  map[string]any{"model": "glm-4.6"},
			wantNext:  map[string]any{trig: "zai", "model": "glm-5.3-flash"},
		},
		{
			name:      "yolo's own unedited value still moves with the selection",
			selection: map[string]any{trig: "zai", "model": "glm-5.3"},
			file:      map[string]any{"model": "glm-4.6"},
			record:    map[string]any{trig: "zai", "model": "glm-4.6"},
			wantLift:  map[string]any{"model": "glm-5.3"},
			wantNext:  map[string]any{trig: "zai", "model": "glm-5.3"},
		},
		{
			name:      "a changed trigger writes over a user edit",
			selection: map[string]any{trig: "bedrock", "model": "global.anthropic.claude-opus-5-5"},
			file:      map[string]any{"model": "claude-opus-5-5"},
			record:    map[string]any{trig: ""},
			wantLift:  map[string]any{"model": "global.anthropic.claude-opus-5-5"},
			wantNext:  map[string]any{trig: "bedrock", "model": "global.anthropic.claude-opus-5-5"},
		},
		{
			// The value is the one yolo last wrote, but the provider changed and the user had
			// picked something else since: the change writes it again.
			name:      "a changed trigger writes even a value equal to the last one yolo wrote",
			selection: map[string]any{trig: "kilo", "model": "opus"},
			file:      map[string]any{"model": "mine"},
			record:    map[string]any{trig: "openrouter", "model": "opus"},
			wantLift:  map[string]any{"model": "opus"},
			wantNext:  map[string]any{trig: "kilo", "model": "opus"},
		},
		{
			// The user chose claude's Default, which removes the key. Re-adding it on the same
			// provider would undo that choice.
			name:      "a key the user removed stays removed while the trigger is unchanged",
			selection: map[string]any{trig: "zai", "model": "glm-5.3"},
			file:      map[string]any{},
			record:    map[string]any{trig: "zai", "model": "glm-5.3"},
			wantLift:  map[string]any{},
			wantNext:  map[string]any{trig: "zai", "model": "glm-5.3"},
		},
		{
			name:      "a key the user removed is written again on a change",
			selection: map[string]any{trig: "bedrock", "model": "opus"},
			file:      map[string]any{},
			record:    map[string]any{trig: "zai", "model": "glm-5.3"},
			wantLift:  map[string]any{"model": "opus"},
			wantNext:  map[string]any{trig: "bedrock", "model": "opus"},
		},
		{
			name:      "a change to no value clears yolo's own value",
			selection: map[string]any{trig: ""},
			file:      map[string]any{"model": "glm-5.3"},
			record:    map[string]any{trig: "zai", "model": "glm-5.3"},
			wantLift:  map[string]any{},
			wantNext:  map[string]any{trig: ""},
			wantClear: []string{"model"},
		},
		{
			// The derive selects nothing on a change only when no value is valid on the new
			// choice, so the user's own value goes too (claude's own login cannot call a z.ai id).
			name:      "a change to no value clears the user's own value too",
			selection: map[string]any{trig: ""},
			file:      map[string]any{"model": "glm-4.6"},
			record:    map[string]any{trig: "zai", "model": "glm-5.3"},
			wantLift:  map[string]any{},
			wantNext:  map[string]any{trig: ""},
			wantClear: []string{"model"},
		},
		{
			name:      "while the trigger is unchanged, no value keeps the user's own value",
			selection: map[string]any{trig: "zai"},
			file:      map[string]any{"model": "glm-4.6"},
			record:    map[string]any{trig: "zai", "model": "glm-5.3"},
			wantLift:  map[string]any{"model": "glm-4.6"},
			wantNext:  map[string]any{trig: "zai", "model": "glm-5.3"},
		},
		{
			// An upgrade, or a first render: no trigger was recorded, so whether the provider
			// changed is unknown, and the unknown reads as a change. A value valid on the
			// provider is its own equivalent, so only a stale one is replaced.
			name:      "no recorded trigger is a change",
			selection: map[string]any{trig: "zai", "model": "glm-5.3"},
			file:      map[string]any{"model": "claude-opus-5-5"},
			wantLift:  map[string]any{"model": "glm-5.3"},
			wantNext:  map[string]any{trig: "zai", "model": "glm-5.3"},
		},
		{
			name:      "a derive that stops emitting the trigger forgets it",
			selection: map[string]any{"model": "glm-5.3"},
			file:      map[string]any{"model": "glm-5.3"},
			record:    map[string]any{trig: "zai", "model": "glm-5.3"},
			wantLift:  map[string]any{"model": "glm-5.3"},
			wantNext:  map[string]any{"model": "glm-5.3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lift, next, cleared := ApplySelectionReport(tc.selection, tc.file, tc.record, nil)
			if !reflect.DeepEqual(lift, tc.wantLift) {
				t.Errorf("lift = %v, want %v", lift, tc.wantLift)
			}
			if !reflect.DeepEqual(next, tc.wantNext) {
				t.Errorf("next = %v, want %v", next, tc.wantNext)
			}
			var keys []string
			for _, c := range cleared {
				keys = append(keys, c.Key)
			}
			if !reflect.DeepEqual(keys, tc.wantClear) {
				t.Errorf("cleared = %v, want %v", keys, tc.wantClear)
			}
			if _, lifted := lift[trig]; lifted {
				t.Error("the trigger was lifted into the file; it belongs to the record alone")
			}
		})
	}
}

// A HOST VALUE IS STILL WRITTEN OVER under a trigger: a value the user's host config supplies
// is not an in-jail pick (OQ-SW1), so the selection outranks it as it does without one.
func TestApplySelectionTriggerOutranksAHostValue(t *testing.T) {
	sel := map[string]any{SelectionTriggerKey: "zai", "model": "glm-5.3"}
	lift, next, _ := ApplySelectionReport(sel, map[string]any{"model": "opus"}, nil, map[string]bool{"model": true})
	if lift["model"] != "glm-5.3" || next["model"] != "glm-5.3" {
		t.Errorf("lift = %v, next = %v, want the selection over the host's value", lift, next)
	}
}
