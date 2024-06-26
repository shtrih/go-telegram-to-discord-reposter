package tgapi

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TestCase struct {
	Text     string
	Entities []tgbotapi.MessageEntity
	Expected string
}

func TestEntities(t *testing.T) {
	cases := map[string]TestCase{
		"italic": {
			Text: "\u3297\ufe0f Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "italic",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "\u3297\ufe0f Lorem _markdownum_ temptabat",
		},
		"bold": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "bold",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "㊗️ Lorem **markdownum** temptabat",
		},
		"underline": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "underline",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "㊗️ Lorem __markdownum__ temptabat",
		},
		"strikethrough": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "strikethrough",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "㊗️ Lorem ~~markdownum~~ temptabat",
		},
		"code": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "code",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "㊗️ Lorem `markdownum` temptabat",
		},
		"pre": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "pre",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "㊗️ Lorem ```markdownum``` temptabat",
		},
		"text_link": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "text_link",
					Offset: 9,
					Length: 10,
					URL:    "https://example.com/",
				},
			},
			Expected: `㊗️ Lorem [markdownum](https://example.com/ "https://example.com/") temptabat`,
		},
		"hashtag": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "hashtag",
					Offset: 9,
					Length: 10,
				},
			},
			Expected: "㊗️ Lorem markdownum temptabat",
		},
		"url": {
			Text: "㊗️ Lorem https://t.me/#foo-bar temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "url",
					Offset: 9,
					Length: 21,
				},
			},
			Expected: "㊗️ Lorem https://t.me/#foo-bar temptabat",
		},
		"escape": {
			Text: "㊗️ *Lorem* _markdownum_ https://t.me/f?foo ~temptabat~ ![lorem](ipsum) __dolor__ >sit amet",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "url",
					Offset: 24,
					Length: 18,
				},
			},
			Expected: "㊗️ \\*Lorem\\* \\_markdownum\\_ https://t.me/f?foo \\~temptabat\\~ \\!\\[lorem\\]\\(ipsum\\) \\_\\_dolor\\_\\_ \\>sit amet",
		},
		"adjacent": {
			Text: "㊗️ Lorem markdownum temptabat",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "bold",
					Offset: 0,
					Length: 8,
				},
				{
					Type:   "italic",
					Offset: 8,
					Length: 11,
				},
			},
			Expected: `**㊗️ Lorem**_ markdownum_ temptabat`,
		},
		"complex": {
			Text: "Lorem markdownum _temptabat usus rapta_ superesse uno segetes reponere decens,\n#carinae ~__*quis*__~.",
			Entities: []tgbotapi.MessageEntity{
				{
					Type:   "bold",
					Offset: 0,
					Length: 5,
				},
				{
					Type:   "italic",
					Offset: 0,
					Length: 5,
				},
				{
					Type:   "italic",
					Offset: 6,
					Length: 10,
				},
				{
					Type:   "underline",
					Offset: 28,
					Length: 4,
				},
				{
					Type:   "strikethrough",
					Offset: 40,
					Length: 9,
				},
				{
					Type:   "code",
					Offset: 50,
					Length: 3,
				},
				{
					Type:   "text_link",
					Offset: 54,
					Length: 7,
					URL:    "https://example.com/",
				},
				{
					Type:   "bold",
					Offset: 54,
					Length: 7,
				},
				{
					Type:   "hashtag",
					Offset: 81,
					Length: 8,
				},
			},
			Expected: "**_Lorem_** _markdownum_ \\_temptabat __usus__ rapta\\_ ~~superesse~~ `uno` [**segetes**](https://example.com/ \"https://example.com/\") reponere decens,\n\\#carinae \\~\\_\\_\\*quis\\*\\_\\_\\~\\.",
		},
	}

	for casename, testcase := range cases {
		t.Run(casename, func(t *testing.T) {
			actual := EntitiesToDiscordMarkdown(testcase.Text, testcase.Entities)
			if actual != testcase.Expected {
				t.Fatalf("\nExpected:\n\"%s\"\n\nGot:\n\"%s\"", testcase.Expected, actual)
			}
		})
	}
}
