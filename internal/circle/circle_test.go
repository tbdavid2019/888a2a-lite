package circle

import "testing"

func TestResolvePublicAndConfiguredCircles(t *testing.T) {
	resolver, err := NewResolver(ModeMulti, "team-a:secret-a,team-b:secret-b", "stable-hub-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	public, err := resolver.Resolve("")
	if err != nil || public.ID != PublicID {
		t.Fatalf("public identity = %+v, err=%v", public, err)
	}
	teamA, err := resolver.Resolve("secret-a")
	if err != nil {
		t.Fatal(err)
	}
	teamAAgain, err := resolver.Resolve("secret-a")
	if err != nil || teamA.ID != teamAAgain.ID {
		t.Fatalf("configured key is not deterministic: %+v / %+v, err=%v", teamA, teamAAgain, err)
	}
	teamB, err := resolver.Resolve("secret-b")
	if err != nil || teamA.ID == teamB.ID {
		t.Fatalf("different keys share a circle: %+v / %+v, err=%v", teamA, teamB, err)
	}
}

func TestResolveRejectsUnlistedKeyWhenDynamicCirclesDisabled(t *testing.T) {
	resolver, err := NewResolver(ModeMulti, "team-a:secret-a", "stable-hub-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("unknown"); err == nil {
		t.Fatal("expected unlisted shared key to be rejected")
	}
}

func TestResolveDynamicCircleIsStable(t *testing.T) {
	resolver, err := NewResolver(ModeMulti, "", "stable-hub-secret", true)
	if err != nil {
		t.Fatal(err)
	}
	first, err := resolver.Resolve("dynamic-secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve("dynamic-secret")
	if err != nil || first.ID != second.ID || first.ID == PublicID {
		t.Fatalf("dynamic identity is not stable/private: %+v / %+v, err=%v", first, second, err)
	}
}

func TestResolverRejectsAmbiguousAliases(t *testing.T) {
	if _, err := NewResolver(ModeMulti, "team-a:secret,team-b:secret", "stable-hub-secret", false); err == nil {
		t.Fatal("expected duplicate secret aliases to be rejected")
	}
}
