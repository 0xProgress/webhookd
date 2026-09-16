package providers

import (
	"net/http"
	"testing"
)

// fakeProvider is a minimal Provider used to exercise the registry
// without importing providers/mock, which imports this package and
// would create an import cycle.
type fakeProvider struct{ name string }

func (f *fakeProvider) Name() string                           { return f.name }
func (f *fakeProvider) Verify(*http.Request, []byte) error     { return nil }
func (f *fakeProvider) EventType(*http.Request, []byte) string { return "" }
func (f *fakeProvider) DeliveryID(*http.Request) string        { return "" }
func (f *fakeProvider) EventID(*http.Request, []byte) string   { return "" }

// withCleanRegistry replaces the package registry with an empty map for
// the duration of the test and restores the original on cleanup. This
// keeps tests from seeing each other's registrations and from seeing
// any provider registered by an init() in the same binary.
func withCleanRegistry(t *testing.T) {
	t.Helper()
	orig := registry
	registry = map[string]Provider{}
	t.Cleanup(func() { registry = orig })
}

func TestRegisterStoresProvider(t *testing.T) {
	withCleanRegistry(t)

	p := &fakeProvider{name: "alpha"}
	Register(p)

	got, ok := Get("alpha")
	if !ok {
		t.Fatal("Get(alpha) reported not found after Register")
	}
	if got != p {
		t.Fatal("Get returned a different provider than was registered")
	}
}

func TestGetUnknownName(t *testing.T) {
	withCleanRegistry(t)

	if _, ok := Get("missing"); ok {
		t.Fatal("Get on an unregistered name reported found")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	withCleanRegistry(t)

	Register(&fakeProvider{name: "dup"})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("second Register with the same name did not panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value has type %T, want string", r)
		}
		const want = "webhookd: provider already registered: dup"
		if msg != want {
			t.Fatalf("panic message = %q, want %q", msg, want)
		}
	}()

	Register(&fakeProvider{name: "dup"})
}

func TestAllEmpty(t *testing.T) {
	withCleanRegistry(t)

	got := All()
	if got == nil {
		t.Fatal("All() returned nil on an empty registry, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("All() = %v on an empty registry, want empty slice", got)
	}
}

func TestAllContents(t *testing.T) {
	withCleanRegistry(t)

	Register(&fakeProvider{name: "alpha"})
	Register(&fakeProvider{name: "bravo"})
	Register(&fakeProvider{name: "charlie"})

	got := All()
	want := []string{"alpha", "bravo", "charlie"}

	// All() makes no ordering promise, so compare as a set.
	if !sameSet(got, want) {
		t.Fatalf("All() = %v, want the set %v", got, want)
	}
}

// sameSet reports whether two string slices contain the same elements,
// ignoring order. Used because All() does not promise an ordering.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}
