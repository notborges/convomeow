package core

import "testing"

func TestReactionEmojiValidation(t *testing.T) {
	for _, emoji := range []string{"👍", "👍🏽", "❤️", "❤", "👨‍👩‍👧‍👦", "🇧🇷", "1️⃣"} {
		if !ValidReactionEmoji(emoji) {
			t.Errorf("rejected %q", emoji)
		}
	}
	for _, emoji := range []string{"", "hello", "👍👍", "\n", "\u200d", "❤️ text"} {
		if ValidReactionEmoji(emoji) {
			t.Errorf("accepted %q", emoji)
		}
	}
}
