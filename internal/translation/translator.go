package translation

import (
	"context"
	"fmt"

	"COM3D2TranslateTool/internal/model"
)

type Item struct {
	ID                 int64
	Type               string
	VoiceID            string
	Role               string
	SourceArc          string
	SourceFile         string
	SourceText         string
	TranslatedText     string
	PolishedText       string
	PreviousSourceText string
	NextSourceText     string
}

type Result struct {
	ID   int64
	Text string
}

type Request struct {
	Settings    model.TranslationSettings
	Items       []Item
	TargetField string
}

type Translator interface {
	Name() string
	Translate(ctx context.Context, req Request) ([]Result, error)
}

type ManualTranslator struct{}

func (ManualTranslator) Name() string {
	return "manual"
}

func (ManualTranslator) Translate(context.Context, Request) ([]Result, error) {
	return nil, fmt.Errorf("manual translator does not support automatic translation")
}
