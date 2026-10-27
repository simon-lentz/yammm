package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestExitError_MessageWithoutAWrappedError(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		code int
		want string
	}{
		{ExitValidation, "validation errors found"},
		{ExitUsage, "usage error"},
		{ExitRuntime, "runtime error"},
	} {
		if got := Exit(tt.code).Error(); got != tt.want {
			t.Errorf("Exit(%d).Error() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestExit_SuccessIsNoErrorAndAnUnnamedCodePanics(t *testing.T) {
	t.Parallel()

	if err := Exit(ExitOK); err != nil {
		t.Errorf("Exit(ExitOK) = %v, want nil", err)
	}
	for _, code := range []int{-1, 4, 256} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Exit(%d) did not panic; the CLI names no such code", code)
				}
			}()
			_ = Exit(code)
		}()
	}
}

func TestExitError_CarriesItsError(t *testing.T) {
	t.Parallel()

	cause := errors.New("resolve path: no such file")
	e := Usage(cause)

	if got := e.Error(); got != cause.Error() {
		t.Errorf("Error() = %q, want the wrapped message %q", got, cause.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("Unwrap does not make the cause reachable")
	}
}

// A classifier names the code of an error that carries none, and only of such
// an error: no failure carries another as its cause.
func TestUsageAndRuntime_ClassifyOnlyAnUnclassifiedError(t *testing.T) {
	t.Parallel()

	if Usage(nil) != nil || Runtime(nil) != nil {
		t.Error("a classifier of nil returned an error")
	}
	cause := errors.New("boom")
	for _, tt := range []struct {
		err  error
		want int
	}{
		{Usage(cause), ExitUsage},
		{Runtime(cause), ExitRuntime},
	} {
		if got := ExitForError(tt.err); got != tt.want {
			t.Errorf("ExitForError(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
	for _, classified := range []error{
		Validationf("a.yammm: unformatted"),
		Exit(ExitValidation),
		fmt.Errorf("ctx: %w", Validationf("a.yammm: unformatted")),
		JoinExitErrors(Validationf("a"), Runtimef("b")),
	} {
		if got := Usage(classified); got != classified { //nolint:errorlint // the same error, not one that matches it
			t.Errorf("Usage(%#v) = %#v, want it returned as it is", classified, got)
		}
		if got := Runtime(classified); got != classified { //nolint:errorlint // the same error, not one that matches it
			t.Errorf("Runtime(%#v) = %#v, want it returned as it is", classified, got)
		}
	}
}

func TestConstructors_CarryCodeAndMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"usage", Usagef("flag --%s applies only to --to %s", "package", "go"), ExitUsage},
		{"runtime", Runtimef("fetch constraints: %v", errors.New("boom")), ExitRuntime},
		{"validation", Validationf("%s: %v", "a.yammm", errors.New("bad syntax")), ExitValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExitForError(tt.err); got != tt.want {
				t.Errorf("ExitForError = %d, want %d", got, tt.want)
			}
			if tt.err.Error() == "" {
				t.Error("the constructor produced an error with no message")
			}
		})
	}
}

// ExitForError's arms are what decide the code a failure reaches the shell as.
// Every one is exercised, the unclassified fallback included: a mutation of
// that constant is invisible without a case that reaches it.
func TestExitForError(t *testing.T) {
	t.Parallel()

	_, statErr := os.Stat(filepath.Join(t.TempDir(), "absent"))
	if statErr == nil {
		t.Fatal("stat of an absent path returned no error")
	}

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is success", nil, ExitOK},
		{"a bare signal keeps its own code", Exit(ExitValidation), ExitValidation},
		{
			"a usage failure wrapping a path error still keeps its code",
			Usage(statErr), ExitUsage,
		},
		{"a real path error is a runtime failure", statErr, ExitRuntime},
		{"the not-exist sentinel is a runtime failure", fs.ErrNotExist, ExitRuntime},
		{"the permission sentinel is a runtime failure", fs.ErrPermission, ExitRuntime},
		{"an operand CheckOperand refuses is a usage failure", CheckOperand("data file", ""), ExitUsage},
		{"a non-UTF-8 source CheckSourceOperand refuses is a usage failure", CheckSourceOperand("data file", "x\xff"), ExitUsage},
		// A location sentinel that no CheckOperand marked came from a
		// resolution the filesystem answered.
		{"a resolution into bytes that are not UTF-8 is a runtime failure", fmt.Errorf("resolve: %w", location.ErrInvalidUTF8Path), ExitRuntime},
		{"a failure the command did not classify is a runtime failure", errors.New("invalid output format"), ExitRuntime},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExitForError(tt.err); got != tt.want {
				t.Errorf("ExitForError = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestCheckOperand_CarriesLocationsSentinel pins that each refusal wraps the
// location sentinel it names, so a caller matches it by errors.Is, and that a
// path which only names a file to read may be any bytes.
func TestCheckOperand_CarriesLocationsSentinel(t *testing.T) {
	t.Parallel()
	if err := CheckOperand("snapshot file", ""); !errors.Is(err, location.ErrEmptyPath) {
		t.Errorf("CheckOperand(\"\") = %v, want ErrEmptyPath", err)
	}
	if err := CheckOperand("snapshot file", "x\xff.ys"); err != nil {
		t.Errorf("CheckOperand refused a name that is not UTF-8: %v", err)
	}
	if err := CheckSourceOperand("data file", ""); !errors.Is(err, location.ErrEmptyPath) {
		t.Errorf("CheckSourceOperand(\"\") = %v, want ErrEmptyPath", err)
	}
	if err := CheckSourceOperand("data file", "x\xff.json"); !errors.Is(err, location.ErrInvalidUTF8Path) {
		t.Errorf("CheckSourceOperand of a name that is not UTF-8 = %v, want ErrInvalidUTF8Path", err)
	}
}

// An unreadable file reaches some commands as a diagnostic rather than an error
// return, so the two rules have to agree or one missing path draws two codes.
func TestExitForResult_RuntimeCodeOutranksValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code diag.Code
		want int
	}{
		{"a schema load I/O failure", diag.E_LOAD_IO_FAILURE, ExitRuntime},
		{"a snapshot I/O failure", diag.E_SNAPSHOT_IO, ExitRuntime},
		{"an adapter I/O failure", diag.E_ADAPTER_IO, ExitRuntime},
		{"an internal fault", diag.E_INTERNAL, ExitRuntime},
		{"a cancelled context", diag.E_CONTEXT_CANCELLED, ExitRuntime},
		{"an ordinary error", diag.E_MISSING_REQUIRED, ExitValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := diag.NewCollectorUnlimited()
			c.Collect(diag.NewIssue(diag.Error, tt.code, "boom").Build())
			if got := ExitForResult(c.Result()); got != tt.want {
				t.Errorf("ExitForResult = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("a clean result is success", func(t *testing.T) {
		t.Parallel()
		if got := ExitForResult(diag.OK()); got != ExitOK {
			t.Errorf("ExitForResult of a clean result = %d, want %d", got, ExitOK)
		}
	})

	// A full collector drops later issues, and a runtime code it dropped still
	// decides: the process failed outside its input whatever the collector kept.
	t.Run("a runtime code the collector dropped still decides", func(t *testing.T) {
		t.Parallel()
		c := diag.NewCollector(1)
		c.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "kept").Build())
		c.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL, "dropped").Build())
		if c.Result().HasCode(diag.E_INTERNAL) {
			t.Fatal("precondition: the collector kept the second issue")
		}
		if got := ExitForResult(c.Result()); got != ExitRuntime {
			t.Errorf("ExitForResult = %d, want %d", got, ExitRuntime)
		}
	})

	t.Run("an I/O code at Warning severity is still a runtime failure", func(t *testing.T) {
		t.Parallel()
		c := diag.NewCollectorUnlimited()
		c.Collect(diag.NewIssue(diag.Warning, diag.E_LOAD_IO_FAILURE, "boom").Build())
		if got := ExitForResult(c.Result()); got != ExitRuntime {
			t.Errorf("ExitForResult = %d, want %d — an I/O code counts at whatever severity the loader chose", got, ExitRuntime)
		}
	})
}

func TestReportError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil writes nothing", nil, ""},
		{
			"a bare exit signal writes nothing",
			Exit(ExitValidation), "",
		},
		{
			"a carried message is prefixed once",
			Usagef("--check and --write are mutually exclusive"),
			"error: --check and --write are mutually exclusive\n",
		},
		{
			"a plain error prints, so cobra's own failures are not silent",
			errors.New(`unknown command "nosuch"`),
			"error: unknown command \"nosuch\"\n",
		},
		{
			"every joined message gets its own line and prefix",
			JoinExitErrors(Validationf("first"), Validationf("second")),
			"error: first\nerror: second\n",
		},
		{
			"each line of one failure gets its own prefix",
			errors.Join(errors.New("first"), errors.New("second")),
			"error: first\nerror: second\n",
		},
		{
			"a trailing newline ends a message and starts no empty one",
			Usagef("one line\n"),
			"error: one line\n",
		},
		{
			"a bare exit signal inside a join adds no line",
			joined{Exit(ExitValidation), errors.New("b.yammm: unreadable")},
			"error: b.yammm: unreadable\n",
		},
		{
			"a failure with no text is reported under its exit code",
			joined{errors.New(""), fmt.Errorf("%.0w%.0w", errors.New("a"), errors.New("b")), Validationf("\n\n"), errors.New("c")},
			"error: runtime error\nerror: runtime error\nerror: validation errors found\nerror: c\n",
		},
		{
			"a failure with no text is reported under the code it exits with",
			Validationf("%w", Runtimef("")),
			"error: validation errors found\n",
		},
		{
			"a text of only white space is no text, and a line of it writes no line",
			joined{Validationf(" \r\n"), errors.New("a\n\r\nb")},
			"error: validation errors found\nerror: a\nerror: b\n",
		},
		{
			"a line keeps its own trailing white space",
			errors.New("unknown flag: --format \nnext\t"),
			"error: unknown flag: --format \nerror: next\t\n",
		},
		{
			"white space beyond space, tab and carriage return is text",
			errors.New("\u00a0"),
			"error: \u00a0\n",
		},
		{
			"a classifier keeps a bare signal bare",
			Runtime(Exit(ExitValidation)),
			"",
		},
		{
			"an empty line inside a failure writes no line",
			fmt.Errorf("ctx: %w", JoinExitErrors(Validationf("a\n"), Validationf("b"))),
			"error: ctx: a\nerror: b\n",
		},
		{
			"a bare exit signal inside another error's join is part of that one failure",
			errors.Join(Exit(ExitValidation), errors.New("b")),
			"error: validation errors found\nerror: b\n",
		},
		{
			"an error that wraps several causes under one message is one failure",
			fmt.Errorf("type %q: %w: %w", "Grid", errors.New("refused"), errors.New("class")),
			"error: type \"Grid\": refused: class\n",
		},
		{
			"a marked refusal is the one failure it marks",
			Validationf("generate node queries: %w", markedRefusal()),
			"error: generate node queries: coerce Integer: cannot convert float64 to int64\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			ReportError(&buf, tt.err)
			if got := buf.String(); got != tt.want {
				t.Errorf("ReportError wrote %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFailureResult pins the one conversion from a failure to a diagnostic: one
// E_COMMAND_FAILED issue per message, each carrying the process exit code, and
// nothing for nil or a bare exit signal.
func TestFailureResult(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		nil, Exit(ExitValidation), JoinExitErrors(Exit(ExitRuntime)), Runtime(Exit(ExitValidation)),
	} {
		if _, ok := FailureResult(err); ok {
			t.Errorf("FailureResult(%v) reported a failure; a bare signal carries no message", err)
		}
	}

	for _, tt := range []struct {
		err  error
		want []string
	}{
		{errors.New(""), []string{"runtime error"}},
		{errors.New("boom\n \n"), []string{"boom"}},
		{errors.New("unknown flag: --format "), []string{"unknown flag: --format "}},
		{Validationf("%s", ""), []string{"validation errors found"}},
		{fmt.Errorf("%w", Validationf("%s", "")), []string{"validation errors found"}},
		{fmt.Errorf("%.0w%.0w", errors.New("a"), errors.New("b")), []string{"runtime error"}},
		{JoinExitErrors(errors.New(""), Usagef("%s", "")), []string{"runtime error", "usage error"}},
	} {
		result, ok := FailureResult(tt.err)
		if !ok {
			t.Errorf("FailureResult(%#v) reported no failure; a failure with no text is still a failure", tt.err)
			continue
		}
		var got []string
		for iss := range result.Issues() {
			got = append(got, iss.Message())
		}
		slices.Sort(got)
		if !slices.Equal(got, tt.want) {
			t.Errorf("FailureResult(%#v) messages = %q, want %q", tt.err, got, tt.want)
		}
	}

	err := JoinExitErrors(Validationf("a.yammm: unformatted\n"), Exit(ExitValidation), Runtimef("open b.yammm: denied"))
	result, ok := FailureResult(err)
	if !ok {
		t.Fatal("FailureResult of two messages reported none")
	}
	var got []string
	for iss := range result.Issues() {
		if iss.Code() != diag.E_COMMAND_FAILED || iss.Severity() != diag.Error {
			t.Errorf("issue %s at %s, want E_COMMAND_FAILED at error", iss.Code(), iss.Severity())
		}
		details := iss.Details()
		if len(details) != 1 || details[0].Key != "exit_code" || details[0].Value != "3" {
			t.Errorf("details = %v, want exit_code 3", details)
		}
		got = append(got, iss.Message())
	}
	slices.Sort(got)
	if want := []string{"a.yammm: unformatted", "open b.yammm: denied"}; !slices.Equal(got, want) {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

// A command that takes a list must not let one entry's failure hide another's.
// The codes do not rank numerically, so the ranking is the thing under test.
func TestJoinExitErrors(t *testing.T) {
	t.Parallel()

	t.Run("no failures is no error", func(t *testing.T) {
		t.Parallel()
		if err := JoinExitErrors(nil, nil); err != nil {
			t.Errorf("JoinExitErrors of no failures = %v, want nil", err)
		}
	})

	t.Run("a validation failure does not mask an I/O failure", func(t *testing.T) {
		t.Parallel()
		err := JoinExitErrors(Validationf("a.yammm is unformatted"), Runtimef("open b.yammm: denied"))
		if err == nil {
			t.Fatal("JoinExitErrors of two failures returned nil")
		}
		if got := ExitForError(err); got != ExitRuntime {
			t.Errorf("exit code = %d, want %d", got, ExitRuntime)
		}
		for _, want := range []string{"a.yammm", "b.yammm"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message does not name %q:\n%s", want, err.Error())
			}
		}
	})

	t.Run("an I/O failure does not mask a usage failure", func(t *testing.T) {
		t.Parallel()
		err := JoinExitErrors(Runtimef("open a: denied"), Usagef("--check and --write are mutually exclusive"))
		if got := ExitForError(err); got != ExitUsage {
			t.Errorf("exit code = %d, want %d — usage outranks an I/O failure", got, ExitUsage)
		}
	})

	t.Run("bare exit signals stay bare", func(t *testing.T) {
		t.Parallel()
		err := JoinExitErrors(Exit(ExitValidation), nil, Exit(ExitValidation))
		if got := ExitForError(err); got != ExitValidation {
			t.Errorf("exit code = %d, want %d", got, ExitValidation)
		}
		if exitErr, ok := errors.AsType[*exitError](err); !ok || exitErr.err != nil {
			t.Errorf("JoinExitErrors of bare signals = %#v, want a bare ExitError", err)
		}
		var buf bytes.Buffer
		ReportError(&buf, err)
		if buf.Len() != 0 {
			t.Errorf("two bare signals reported %q; each command already rendered what it had", buf.String())
		}
	})

	t.Run("order does not decide the code", func(t *testing.T) {
		t.Parallel()
		forwards := JoinExitErrors(Validationf("v"), Runtimef("r"))
		backwards := JoinExitErrors(Runtimef("r"), Validationf("v"))
		if ExitForError(forwards) != ExitForError(backwards) {
			t.Errorf("order changed the code: %d then %d",
				ExitForError(forwards), ExitForError(backwards))
		}
	})
}

// markedRefusal returns adapter/neo4j's refusal of a float at an Integer, which
// wraps the coercion error and the adapter's class.
func markedRefusal() error {
	_, err := adaptern4j.Coerce(schema.NewIntegerConstraint(), 2.5)
	return err
}
