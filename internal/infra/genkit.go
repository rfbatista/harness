package infra

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/compat_oai/openai"
	"github.com/firebase/genkit/go/plugins/ollama"
)

var (
	genkitOnce sync.Once
	genkitInst *genkit.Genkit
)

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// GenkitInstance returns a singleton Genkit instance initialized with OpenAI
// and Ollama plugins, with an explicit Ollama model registration for tool use.
func GenkitInstance(ctx context.Context) *genkit.Genkit {
	genkitOnce.Do(func() {
		openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		openAIModel := envOr("GENKIT_OPENAI_MODEL", "openai/gpt-4o")
		ollamaURL := envOr("OLLAMA_SERVER_URL", "http://127.0.0.1:11434")
		ollamaModel := strings.TrimPrefix(envOr("GENKIT_OLLAMA_MODEL", "qwen3-coder:latest"), "ollama/")

		defaultModel := envOr("GENKIT_DEFAULT_MODEL", openAIModel)
		if openAIKey == "" {
			defaultModel = envOr("GENKIT_DEFAULT_MODEL", ollamaModel)
		}

		slog.Info("initializing Genkit",
			"default_model", defaultModel,
			"openai_model", openAIModel,
			"ollama_model", ollamaModel,
			"ollama_url", ollamaURL,
			"openai_enabled", openAIKey != "",
		)

		ollamaPlugin := &ollama.Ollama{
			ServerAddress: ollamaURL,
			Timeout:       120,
		}

		ollamaModelOption := &ai.ModelOptions{
			Supports: &ai.ModelSupports{
				Multiturn:  true,
				SystemRole: true,
				Tools:      true,
				Media:      false,
			},
		}

		if openAIKey != "" {
			openAIPlugin := &openai.OpenAI{APIKey: openAIKey}
			genkitInst = genkit.Init(ctx,
				genkit.WithPlugins(openAIPlugin, ollamaPlugin),
				genkit.WithDefaultModel(defaultModel),
			)
		} else {
			genkitInst = genkit.Init(ctx,
				genkit.WithPlugins(ollamaPlugin),
				genkit.WithDefaultModel(defaultModel),
			)
		}

		ollamaPlugin.DefineModel(
			genkitInst,
			ollama.ModelDefinition{
				Name: ollamaModel,
				Type: "chat",
			},
			ollamaModelOption,
		)

		slog.Info("Genkit initialized successfully")
	})
	return genkitInst
}
