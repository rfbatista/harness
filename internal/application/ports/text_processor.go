package ports

import "context"

type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
}

type TextGenerator interface {
	GenerateText(ctx context.Context, prompt string) (string, error)
}

type Translator interface {
	Translate(ctx context.Context, text, targetLanguage string) (string, error)
}
