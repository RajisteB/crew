package mission

import (
	"testing"

	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/source"
)

func sw(modes map[string]principal.AutoMode) map[string]principal.SwitchRecord {
	out := map[string]principal.SwitchRecord{}
	for d, m := range modes {
		out[d] = principal.SwitchRecord{Domain: d, Mode: m}
	}
	return out
}

func testDomains() map[string]source.Domain {
	return map[string]source.Domain{
		"product":   {Name: "product", Paths: []string{"app/**"}},
		"design":    {Name: "design", Paths: []string{"brand/**"}},
		"marketing": {Name: "marketing"},
	}
}

func TestEffectiveMode_OwnDomainOff(t *testing.T) {
	got := EffectiveMode(Piece{Domain: "product"}, []string{"app/x.go", "brand/a.svg"}, testDomains(),
		sw(map[string]principal.AutoMode{"product": principal.AutoOff, "design": principal.AutoOn}))
	if got != (AutoObs{Mode: principal.AutoOff}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveMode_OtherDomainOffBlocks(t *testing.T) {
	got := EffectiveMode(Piece{Domain: "product"}, []string{"app/x.go", "brand/logo.svg"}, testDomains(),
		sw(map[string]principal.AutoMode{"product": principal.AutoOn, "design": principal.AutoOff}))
	if got != (AutoObs{Mode: principal.AutoOff, Blocker: "design"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveMode_FirstBlockerByName(t *testing.T) {
	doms := testDomains()
	doms["zeta"] = source.Domain{Name: "zeta", Paths: []string{"z/**"}}
	got := EffectiveMode(Piece{Domain: "product"}, []string{"z/a", "brand/a"}, doms,
		sw(map[string]principal.AutoMode{"product": principal.AutoOn}))
	if got != (AutoObs{Mode: principal.AutoOff, Blocker: "design"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveMode_UntouchedDomainIgnored(t *testing.T) {
	got := EffectiveMode(Piece{Domain: "product"}, []string{"app/x.go"}, testDomains(),
		sw(map[string]principal.AutoMode{"product": principal.AutoOn, "design": principal.AutoOff}))
	if got != (AutoObs{Mode: principal.AutoOn}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveMode_StrictestOnWins(t *testing.T) {
	files := []string{"app/x.go", "brand/logo.svg"}
	got := EffectiveMode(Piece{Domain: "product"}, files, testDomains(),
		sw(map[string]principal.AutoMode{"product": principal.AutoWithoutChecks, "design": principal.AutoOn}))
	if got.Mode != principal.AutoOn {
		t.Fatalf("got %+v", got)
	}
	got = EffectiveMode(Piece{Domain: "product"}, files, testDomains(),
		sw(map[string]principal.AutoMode{"product": principal.AutoWithoutChecks, "design": principal.AutoWithoutChecks}))
	if got != (AutoObs{Mode: principal.AutoWithoutChecks}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveMode_DomainWithoutPathsNeverTouched(t *testing.T) {
	got := EffectiveMode(Piece{Domain: "product"}, []string{"app/x.go", "marketing/a"}, testDomains(),
		sw(map[string]principal.AutoMode{"product": principal.AutoOn, "marketing": principal.AutoOff}))
	if got != (AutoObs{Mode: principal.AutoOn}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveMode_MissingSwitchIsOff(t *testing.T) {
	got := EffectiveMode(Piece{Domain: "product"}, nil, testDomains(), nil)
	if got != (AutoObs{Mode: principal.AutoOff}) {
		t.Fatalf("got %+v", got)
	}
}
