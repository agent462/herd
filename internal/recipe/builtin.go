package recipe

import "github.com/agent462/herd/internal/config"

// BuiltinRecipes returns all built-in recipes keyed by name.
func BuiltinRecipes() map[string]config.Recipe {
	return map[string]config.Recipe{
		"disk-check":    builtinDiskCheck(),
		"uptime":        builtinUptime(),
		"reboot-check":  builtinRebootCheck(),
		"service-check": builtinServiceCheck(),
		"port-check":    builtinPortCheck(),
		"user-audit":    builtinUserAudit(),
		"log-tail":      builtinLogTail(),
		"os-version":    builtinOSVersion(),
	}
}

// IsBuiltin reports whether name is a built-in recipe.
func IsBuiltin(name string) bool {
	_, ok := BuiltinRecipes()[name]
	return ok
}

// ResolveRecipe looks up a recipe by name. User-defined recipes in cfg
// override built-ins. Returns the recipe, whether a built-in exists for
// that name, and whether the recipe was found at all.
func ResolveRecipe(name string, cfg *config.Config) (config.Recipe, bool, bool) {
	_, isBuiltin := BuiltinRecipes()[name]

	if cfg != nil {
		if r, ok := cfg.Recipes[name]; ok {
			return r, isBuiltin, true
		}
	}

	if isBuiltin {
		return BuiltinRecipes()[name], true, true
	}

	return config.Recipe{}, false, false
}

// MergedRecipes returns built-in recipes merged with user-defined recipes.
// User recipes override built-ins with the same name.
func MergedRecipes(cfg *config.Config) map[string]config.Recipe {
	merged := make(map[string]config.Recipe)
	for name, r := range BuiltinRecipes() {
		merged[name] = r
	}
	if cfg != nil {
		for name, r := range cfg.Recipes {
			merged[name] = r
		}
	}
	return merged
}

// --- individual built-in recipes ---

func builtinDiskCheck() config.Recipe {
	return config.Recipe{
		Description: "Check disk usage on root filesystem",
		Steps:       []string{"df -h /"},
	}
}

func builtinUptime() config.Recipe {
	return config.Recipe{
		Description: "Show uptime and load averages",
		Steps:       []string{"uptime"},
	}
}

func builtinRebootCheck() config.Recipe {
	return config.Recipe{
		Description: "Check if hosts require a reboot",
		Steps: []string{
			`test -f /var/run/reboot-required && echo "REBOOT REQUIRED" || echo "no reboot needed"`,
		},
	}
}

func builtinServiceCheck() config.Recipe {
	return config.Recipe{
		Description: "Check sshd status; drill into hosts that differ",
		Steps: []string{
			"systemctl is-active sshd",
			"@differs systemctl status sshd --no-pager",
		},
	}
}

func builtinPortCheck() config.Recipe {
	return config.Recipe{
		Description: "List listening TCP ports (ss with netstat fallback)",
		Steps: []string{
			"ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null",
		},
	}
}

func builtinUserAudit() config.Recipe {
	return config.Recipe{
		Description: "List users with login shells",
		Steps: []string{
			`grep -v -e '/nologin$' -e '/false$' /etc/passwd | cut -d: -f1,7`,
		},
	}
}

func builtinLogTail() config.Recipe {
	return config.Recipe{
		Description: "Show recent error log entries",
		Steps: []string{
			"journalctl -p err --no-pager -n 20 2>/dev/null || tail -20 /var/log/syslog 2>/dev/null || tail -20 /var/log/messages",
		},
	}
}

func builtinOSVersion() config.Recipe {
	return config.Recipe{
		Description: "Show OS version across fleet",
		Steps: []string{
			`grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"' || uname -sr`,
		},
	}
}
