package location

import "testing"

func TestRelatedInfo_IsValid(t *testing.T) {
	source := NewSourceID("test://unit")

	tests := []struct {
		name string
		info RelatedInfo
		want bool
	}{
		{
			name: "valid span and message",
			info: RelatedInfo{
				Span:    Point(source, 10, 5),
				Message: MsgPreviousDefinition,
			},
			want: true,
		},
		{
			name: "valid span, no message",
			info: RelatedInfo{
				Span: Point(source, 10, 5),
			},
			want: true,
		},
		{
			name: "no span, valid message",
			info: RelatedInfo{
				Message: MsgPreviousDefinition,
			},
			want: true,
		},
		{
			name: "no span, no message",
			info: RelatedInfo{},
			want: false,
		},
		{
			name: "zero span, no message",
			info: RelatedInfo{
				Span: Span{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestRelatedInfo_String(t *testing.T) {
	source := NewSourceID("test://unit")

	tests := []struct {
		name string
		info RelatedInfo
		want string
	}{
		{
			name: "span and message",
			info: RelatedInfo{
				Span:    Point(source, 10, 5),
				Message: MsgPreviousDefinition,
			},
			want: "test://unit:10:5: previous definition here",
		},
		{
			name: "span only",
			info: RelatedInfo{
				Span: Point(source, 10, 5),
			},
			want: "test://unit:10:5",
		},
		{
			name: "message only",
			info: RelatedInfo{
				Message: MsgPreviousDefinition,
			},
			want: "previous definition here",
		},
		{
			name: "empty",
			info: RelatedInfo{},
			want: "", // When span is zero, returns message (which is empty)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.String()
			if got != tt.want {
				t.Errorf("String() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestMessageConstants(t *testing.T) {
	// Verify constants are as expected
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"MsgPreviousDefinition", MsgPreviousDefinition, "previous definition here"},
		{"MsgImportedFrom", MsgImportedFrom, "imported from here"},
		{"MsgDeclaredHere", MsgDeclaredHere, "declared here"},
		{"MsgRequiredBy", MsgRequiredBy, "required by this constraint"},
		{"MsgReferencedFrom", MsgReferencedFrom, "referenced from here"},
		{"MsgDefinedHere", MsgDefinedHere, "defined here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %q; want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}
