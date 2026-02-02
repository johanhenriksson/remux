package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
)

var templatePattern = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// EvaluateTemplate evaluates all {{ expr }} patterns in the input string.
func EvaluateTemplate(input string, space Space) (string, error) {
	env := map[string]any{
		"space": map[string]any{
			"Name": space.Name,
			"Path": space.Path,
			"Port": space.Port,
			"ID":   space.ID,
		},
		"env": getEnvMap(),
	}

	var evalErr error
	result := templatePattern.ReplaceAllStringFunc(input, func(match string) string {
		if evalErr != nil {
			return match
		}

		// Extract expression from {{ ... }}
		groups := templatePattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		expression := strings.TrimSpace(groups[1])

		// Evaluate with expr-lang
		program, err := expr.Compile(expression, expr.Env(env))
		if err != nil {
			evalErr = fmt.Errorf("invalid expression %q: %w", expression, err)
			return match
		}

		output, err := expr.Run(program, env)
		if err != nil {
			evalErr = fmt.Errorf("failed to evaluate %q: %w", expression, err)
			return match
		}

		return fmt.Sprintf("%v", output)
	})

	if evalErr != nil {
		return "", evalErr
	}
	return result, nil
}

// getEnvMap returns all environment variables as a map.
func getEnvMap() map[string]any {
	result := make(map[string]any)
	for _, kv := range os.Environ() {
		if key, value, ok := strings.Cut(kv, "="); ok {
			result[key] = value
		}
	}
	return result
}
