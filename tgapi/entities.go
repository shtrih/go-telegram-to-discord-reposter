package tgapi

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"unicode/utf16"
)

// Copyright (c) 2020 ratpoison4
// https://github.com/ratpoison4/entities/blob/master/LICENSE
// Original code: https://github.com/ratpoison4/entities/blob/master/entities.go

var needEscape = make(map[rune]struct{})

func init() {
	for _, r := range []rune{'_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'} {
		needEscape[r] = struct{}{}
	}
}

// EntitiesToDiscordMarkdown converts plain text with Entities to Markdown and escapes Markdown special symbols (but it's not escapes those symbols in urls).
// https://core.telegram.org/bots/api#messageentity
func EntitiesToDiscordMarkdown(text string, messageEntities []tgbotapi.MessageEntity) string {
	insertions := make(map[int]string)
	noEscape := make(map[int]*struct{})
	strct := struct{}{}
	stopEscape := func(e *tgbotapi.MessageEntity) {
		for i := e.Offset; i < e.Offset+e.Length; i++ {
			noEscape[i] = &strct
		}
	}

	for _, e := range messageEntities {
		var before, after string

		if e.IsBold() {
			before = "**"
			after = "**"
		} else if e.IsItalic() {
			before = "_"
			after = "_"
		} else if e.Type == "underline" {
			before = "__"
			after = "__"
		} else if e.Type == "strikethrough" {
			before = "~~"
			after = "~~"
		} else if e.IsCode() {
			before = "`"
			after = "`"
			stopEscape(&e)
		} else if e.IsPre() {
			before = "```" + e.Language
			after = "```"
			stopEscape(&e)
		} else if e.IsTextLink() {
			before = "["
			after = fmt.Sprintf(`](%s "%s")`, e.URL, e.URL)
		} else if e.IsURL() {
			stopEscape(&e)
		}
		if before != "" {
			insertions[e.Offset] += before
			insertions[e.Offset+e.Length] += after
		}
	}

	input := []rune(text)
	var output []rune
	utf16pos := 0
	for _, c := range input {
		output = append(output, []rune(insertions[utf16pos])...)
		_, stopEscaping := noEscape[utf16pos]
		if _, has := needEscape[c]; has && !stopEscaping {
			output = append(output, '\\')
		}
		output = append(output, c)
		utf16pos += len(utf16.Encode([]rune{c}))
	}
	output = append(output, []rune(insertions[utf16pos])...)
	return string(output)
}