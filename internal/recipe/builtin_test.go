package recipe

import (
	"testing"

	"github.com/agent462/herd/internal/config"
)

var expectedBuiltins = []string{
	"disk-check",
	"uptime",
	"reboot-check",
	"service-check",
	"port-check",
	"user-audit",
	"log-tail",
	"os-version",
}

func TestBuiltinRecipes_AllPresent(t *testing.T) {
	builtins := BuiltinRecipes()
	if len(builtins) != len(expectedBuiltins) {
		t.Errorf("expected %d built-in recipes, got %d", len(expectedBuiltins), len(builtins))
	}
	for _, name := range expectedBuiltins {
		r, ok := builtins[name]
		if !ok {
			t.Errorf("missing built-in recipe %q", name)
			continue
		}
		if r.Description == "" {
			t.Errorf("recipe %q has empty description", name)
		}
		if len(r.Steps) == 0 {
			t.Errorf("recipe %q has no steps", name)
		}
	}
}

func TestIsBuiltin(t *testing.T) {
	for _, name := range expectedBuiltins {
		if !IsBuiltin(name) {
			t.Errorf("IsBuiltin(%q) = false, want true", name)
		}
	}

	if IsBuiltin("nonexistent") {
		t.Error("IsBuiltin(\"nonexistent\") = true, want false")
	}
	if IsBuiltin("") {
		t.Error("IsBuiltin(\"\") = true, want false")
	}
}

func TestResolveRecipe_BuiltinFound(t *testing.T) {
	cfg := &config.Config{
		Recipes: map[string]config.Recipe{},
	}

	r, isBuiltin, found := ResolveRecipe("uptime", cfg)
	if !found {
		t.Fatal("expected recipe to be found")
	}
	if !isBuiltin {
		t.Error("expected isBuiltin = true")
	}
	if r.Description == "" {
		t.Error("expected non-empty description")
	}
}

func TestResolveRecipe_UserOverridesBuiltin(t *testing.T) {
	userRecipe := config.Recipe{
		Description: "my custom uptime",
		Steps:       []string{"uptime -s"},
	}
	cfg := &config.Config{
		Recipes: map[string]config.Recipe{
			"uptime": userRecipe,
		},
	}

	r, isBuiltin, found := ResolveRecipe("uptime", cfg)
	if !found {
		t.Fatal("expected recipe to be found")
	}
	if !isBuiltin {
		t.Error("expected isBuiltin = true (built-in exists even though overridden)")
	}
	if r.Description != "my custom uptime" {
		t.Errorf("expected user description, got %q", r.Description)
	}
	if len(r.Steps) != 1 || r.Steps[0] != "uptime -s" {
		t.Errorf("expected user steps, got %v", r.Steps)
	}
}

func TestResolveRecipe_UserOnly(t *testing.T) {
	userRecipe := config.Recipe{
		Description: "custom recipe",
		Steps:       []string{"echo hi"},
	}
	cfg := &config.Config{
		Recipes: map[string]config.Recipe{
			"my-recipe": userRecipe,
		},
	}

	r, isBuiltin, found := ResolveRecipe("my-recipe", cfg)
	if !found {
		t.Fatal("expected recipe to be found")
	}
	if isBuiltin {
		t.Error("expected isBuiltin = false")
	}
	if r.Description != "custom recipe" {
		t.Errorf("expected user description, got %q", r.Description)
	}
}

func TestResolveRecipe_NotFound(t *testing.T) {
	cfg := &config.Config{
		Recipes: map[string]config.Recipe{},
	}

	_, _, found := ResolveRecipe("nonexistent", cfg)
	if found {
		t.Error("expected recipe not to be found")
	}
}

func TestResolveRecipe_NilConfig(t *testing.T) {
	r, isBuiltin, found := ResolveRecipe("uptime", nil)
	if !found {
		t.Fatal("expected built-in recipe to be found with nil config")
	}
	if !isBuiltin {
		t.Error("expected isBuiltin = true")
	}
	if r.Description == "" {
		t.Error("expected non-empty description")
	}

	_, _, found = ResolveRecipe("nonexistent", nil)
	if found {
		t.Error("expected recipe not to be found with nil config")
	}
}

func TestMergedRecipes_ContainsBoth(t *testing.T) {
	cfg := &config.Config{
		Recipes: map[string]config.Recipe{
			"my-recipe": {
				Description: "user recipe",
				Steps:       []string{"echo custom"},
			},
		},
	}

	merged := MergedRecipes(cfg)

	// Should contain all built-ins.
	for _, name := range expectedBuiltins {
		if _, ok := merged[name]; !ok {
			t.Errorf("merged map missing built-in %q", name)
		}
	}

	// Should contain user recipe.
	if _, ok := merged["my-recipe"]; !ok {
		t.Error("merged map missing user recipe \"my-recipe\"")
	}
}

func TestMergedRecipes_UserOverrideWins(t *testing.T) {
	cfg := &config.Config{
		Recipes: map[string]config.Recipe{
			"uptime": {
				Description: "overridden",
				Steps:       []string{"uptime -s"},
			},
		},
	}

	merged := MergedRecipes(cfg)
	r := merged["uptime"]
	if r.Description != "overridden" {
		t.Errorf("user override not applied, description = %q", r.Description)
	}
}

func TestMergedRecipes_NilConfig(t *testing.T) {
	merged := MergedRecipes(nil)
	if len(merged) != len(expectedBuiltins) {
		t.Errorf("expected %d recipes with nil config, got %d", len(expectedBuiltins), len(merged))
	}
}

func TestBuiltinRecipes_StepsParse(t *testing.T) {
	for name, r := range BuiltinRecipes() {
		for i, raw := range r.Steps {
			step := ParseStep(raw)
			if step.Command == "" {
				t.Errorf("recipe %q step %d: ParseStep produced empty command from %q", name, i, raw)
			}
		}
	}
}
