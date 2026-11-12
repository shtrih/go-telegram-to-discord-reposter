package tgapi

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func ent(typ string, offset, length int) tgbotapi.MessageEntity {
	return tgbotapi.MessageEntity{Type: typ, Offset: offset, Length: length}
}

func withURL(e tgbotapi.MessageEntity, url string) []tgbotapi.MessageEntity {
	e.URL = url
	return []tgbotapi.MessageEntity{e}
}

func TestEntitiesToDiscordMarkdown(t *testing.T) {
	lorem := "㊗️ Lorem markdownum temptabat"

	tests := []struct {
		name     string
		text     string
		entities []tgbotapi.MessageEntity
		expected string
	}{
		{
			name:     "escape punctuation without entities",
			text:     "㊗️ *Lorem* _markdownum_ ~temptabat~ ![lorem](ipsum) __dolor__ >sit",
			expected: "㊗️ \\*Lorem\\* \\_markdownum\\_ \\~temptabat\\~ \\!\\[lorem\\]\\(ipsum\\) \\_\\_dolor\\_\\_ \\>sit",
		},
		{
			name:     "escape backslash",
			text:     `C:\path`,
			expected: `C:\\path`,
		},
		{
			name:     "bold",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("bold", 9, 10)},
			expected: "㊗️ Lorem **markdownum** temptabat",
		},
		{
			name:     "italic",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("italic", 9, 10)},
			expected: "㊗️ Lorem *markdownum* temptabat",
		},
		{
			name:     "underline",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("underline", 9, 10)},
			expected: "㊗️ Lorem __markdownum__ temptabat",
		},
		{
			name:     "strikethrough",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("strikethrough", 9, 10)},
			expected: "㊗️ Lorem ~~markdownum~~ temptabat",
		},
		{
			name:     "spoiler",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("spoiler", 9, 10)},
			expected: "㊗️ Lorem ||markdownum|| temptabat",
		},
		{
			name:     "code",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("code", 9, 10)},
			expected: "㊗️ Lorem `markdownum` temptabat",
		},
		{
			name:     "code keeps punctuation raw",
			text:     "x a*b(c) y",
			entities: []tgbotapi.MessageEntity{ent("code", 2, 6)},
			expected: "x `a*b(c)` y",
		},
		{
			name:     "pre without language",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("pre", 9, 10)},
			expected: "㊗️ Lorem ```\nmarkdownum\n``` temptabat",
		},
		{
			name: "pre with language",
			text: lorem,
			entities: func() []tgbotapi.MessageEntity {
				e := ent("pre", 9, 10)
				e.Language = "go"
				return []tgbotapi.MessageEntity{e}
			}(),
			expected: "㊗️ Lorem ```go\nmarkdownum\n``` temptabat",
		},
		{
			name: "formatting inside code is dropped",
			text: "a b c",
			entities: []tgbotapi.MessageEntity{
				ent("bold", 0, 1),
				ent("code", 0, 5),
			},
			expected: "`a b c`",
		},
		{
			name:     "text_link",
			text:     lorem,
			entities: withURL(ent("text_link", 9, 10), "https://example.com/"),
			expected: "㊗️ Lorem [markdownum](https://example.com/) temptabat",
		},
		{
			name:     "text_link url parens are percent-encoded",
			text:     lorem,
			entities: withURL(ent("text_link", 9, 10), "https://en.wikipedia.org/wiki/Go_(programming_language)"),
			expected: "㊗️ Lorem [markdownum](https://en.wikipedia.org/wiki/Go_%28programming_language%29) temptabat",
		},
		{
			name:     "text_link without url renders plain",
			text:     "a _b_ c",
			entities: []tgbotapi.MessageEntity{ent("text_link", 2, 1)},
			expected: "a \\_b\\_ c",
		},
		{
			name:     "url entity text is not escaped",
			text:     "㊗️ Lorem https://t.me/#foo-bar temptabat",
			entities: []tgbotapi.MessageEntity{ent("url", 9, 21)},
			expected: "㊗️ Lorem https://t.me/#foo-bar temptabat",
		},
		{
			name:     "email entity text is not escaped",
			text:     "mail me: a.b@example.com",
			entities: []tgbotapi.MessageEntity{ent("email", 9, 15)},
			expected: "mail me: a.b@example.com",
		},
		{
			name:     "plain entities render as-is",
			text:     lorem,
			entities: []tgbotapi.MessageEntity{ent("hashtag", 9, 10)},
			expected: "㊗️ Lorem markdownum temptabat",
		},
		{
			name:     "mention renders as-is",
			text:     "@user hello",
			entities: []tgbotapi.MessageEntity{ent("mention", 0, 5)},
			expected: "@user hello",
		},
		{
			name:     "overlapping same kind merged",
			text:     "abcdefgh",
			entities: []tgbotapi.MessageEntity{ent("bold", 0, 5), ent("bold", 3, 5)},
			expected: "**abcdefgh**",
		},
		{
			name:     "adjacent spoilers merged",
			text:     "abcdef",
			entities: []tgbotapi.MessageEntity{ent("spoiler", 0, 3), ent("spoiler", 3, 3)},
			expected: "||abcdef||",
		},
		{
			name:     "adjacent italics do not collide",
			text:     "abcdef",
			entities: []tgbotapi.MessageEntity{ent("italic", 0, 3), ent("italic", 3, 3)},
			expected: "*abcdef*",
		},
		{
			name:     "bold and italic with same span combine",
			text:     "abc",
			entities: []tgbotapi.MessageEntity{ent("bold", 0, 3), ent("italic", 0, 3)},
			expected: "***abc***",
		},
		{
			name:     "adjacent bold then italic",
			text:     "abcdef",
			entities: []tgbotapi.MessageEntity{ent("bold", 0, 3), ent("italic", 3, 3)},
			expected: "**abc***def*",
		},
		{
			name:     "nested italic inside bold",
			text:     "abcdefghij",
			entities: []tgbotapi.MessageEntity{ent("bold", 0, 10), ent("italic", 2, 3)},
			expected: "**ab*cde*fghij**",
		},
		{
			name:     "nested bold inside italic",
			text:     "abcdefghij",
			entities: []tgbotapi.MessageEntity{ent("italic", 0, 10), ent("bold", 2, 3)},
			expected: "*ab**cde**fghij*",
		},
		{
			name:     "underline and strikethrough with same span",
			text:     "abc",
			entities: []tgbotapi.MessageEntity{ent("underline", 0, 3), ent("strikethrough", 0, 3)},
			expected: "__~~abc~~__",
		},
		{
			name:     "blockquote prefixes every line",
			text:     "line1\nline2\nline3",
			entities: []tgbotapi.MessageEntity{ent("blockquote", 0, 12)},
			expected: "> line1\n> line2\nline3",
		},
		{
			name:     "expandable blockquote like blockquote",
			text:     "line1\nline2",
			entities: []tgbotapi.MessageEntity{ent("expandable_blockquote", 0, 11)},
			expected: "> line1\n> line2",
		},
		{
			name: "blockquote with bold inside",
			text: "quoted bold tail",
			entities: []tgbotapi.MessageEntity{
				ent("blockquote", 0, 16),
				ent("bold", 7, 4),
			},
			expected: "> quoted **bold** tail",
		},
		{
			name: "text_link is outermost at equal range",
			text: lorem,
			entities: []tgbotapi.MessageEntity{
				ent("bold", 9, 10),
				withURL(ent("text_link", 9, 10), "https://example.com/")[0],
			},
			expected: "㊗️ Lorem [**markdownum**](https://example.com/) temptabat",
		},
		{
			name: "adjacent bold then italic across utf16 offset",
			text: lorem,
			entities: []tgbotapi.MessageEntity{
				ent("bold", 0, 8),
				ent("italic", 8, 3),
			},
			expected: "**㊗️ Lorem*** ma*rkdownum temptabat",
		},
		{
			name:     "astral plane offsets",
			text:     "𝕊𝕚 abc",
			entities: []tgbotapi.MessageEntity{ent("bold", 5, 3)},
			expected: "𝕊𝕚 **abc**",
		},
		{
			name:     "composite emoji offsets",
			text:     "👨‍👩‍👧 abc",
			entities: []tgbotapi.MessageEntity{ent("bold", 9, 3)},
			expected: "👨‍👩‍👧 **abc**",
		},
		{
			name:     "entity is clamped to text length",
			text:     "abc",
			entities: []tgbotapi.MessageEntity{ent("bold", 2, 8)},
			expected: "ab**c**",
		},
		{
			name:     "entity beyond text is dropped",
			text:     "abc",
			entities: []tgbotapi.MessageEntity{ent("bold", 5, 2)},
			expected: "abc",
		},
		{
			name: "complex mix",
			text: "Lorem markdownum _temptabat usus rapta_ superesse uno segetes reponere decens,\n#carinae ~__*quis*__~.",
			entities: func() []tgbotapi.MessageEntity {
				link := ent("text_link", 54, 7)
				link.URL = "https://example.com/"
				return []tgbotapi.MessageEntity{
					ent("bold", 0, 5),
					ent("italic", 0, 5),
					ent("italic", 6, 10),
					ent("underline", 28, 4),
					ent("strikethrough", 40, 9),
					ent("code", 50, 3),
					link,
					ent("bold", 54, 7),
					ent("hashtag", 79, 8),
				}
			}(),
			expected: "***Lorem*** *markdownum* \\_temptabat __usus__ rapta\\_ ~~superesse~~ `uno` [**segetes**](https://example.com/) reponere decens,\n\\#carinae \\~\\_\\_\\*quis\\*\\_\\_\\~\\.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EntitiesToDiscordMarkdown(tc.text, tc.entities); got != tc.expected {
				t.Fatalf("\nexpected: %q\n     got: %q", tc.expected, got)
			}
		})
	}
}
