package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
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
		{99, "exit"},
	} {
		if got := (&ExitError{Code: tt.code}).Error(); got != tt.want {
			t.Errorf("ExitError{Code: %d}.Error() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestExitError_CarriesItsError(t *testing.T) {
	t.Parallel()

	cause := errors.New("resolve path: no such file")
	e := &ExitError{Code: ExitUsage, Err: cause}

	if got := e.Error(); got != cause.Error() {
		t.Errorf("Error() = %q, want the wrapped message %q", got, cause.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("Unwrap does not make the cause reachable")
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
		{"an ExitError keeps its own code", &ExitError{Code: ExitValidation}, ExitValidation},
		{
			"an ExitError wrapping a path error still keeps its code",
			&ExitError{Code: ExitUsage, Err: statErr}, ExitUsage,
		},
		{"a real path error is a runtime failure", statErr, ExitRuntime},
		// The classification is by identity, not by wording: an error whose
		// text happens to read like a missing file is not one.
		{
			"a message that merely reads like not-exist is not an I/O failure",
			errors.New("x: " + fs.ErrNotExist.Error()), ExitUsage,
		},
		{"the not-exist sentinel is a runtime failure", fs.ErrNotExist, ExitRuntime},
		{"the permission sentinel is a runtime failure", fs.ErrPermission, ExitRuntime},
		{"anything else falls back to usage", errors.New("invalid output format"), ExitUsage},
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

// An unreadable file reaches some commands as a diagnostic rather than an error
// return, so the two rules have to agree or one missing path draws two codes.
func TestExitForResult_IOCodeOutranksValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code diag.Code
		want int
	}{
		{"a schema load I/O failure", diag.E_LOAD_IO_FAILURE, ExitRuntime},
		{"a snapshot I/O failure", diag.E_SNAPSHOT_IO, ExitRuntime},
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

	t.Run("a warning is not a failure", func(t *testing.T) {
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
			&ExitError{Code: ExitValidation}, "",
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
			errors.Join(errors.New("first"), errors.New("second")),
			"error: first\nerror: second\n",
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
