package card

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

func TestRender(t *testing.T) {
	cases := map[string]Match{
		"live":    {Prompt: "5'10 is short.", NameA: "Marcus", NameB: "Priya", TierA: "NPC", TierB: "Cooker", Status: "live", CrowdA: 3, CrowdB: 7},
		"done":    {Prompt: "Being born is being forced to work.", NameA: "Jake", NameB: "cookbot", TierA: "Chud", TierB: "NPC", Status: "done", Winner: "b", Headline: "cookbot cooked Jake", ScoreA: 28},
		"waiting": {Prompt: "Marcus can't cook and everyone in the group chat knows it but nobody will say it to his face except me", NameA: "Sam", TierA: "Goat", Status: "waiting"},
	}
	for name, m := range cases {
		var buf bytes.Buffer
		if err := Render(&buf, m); err != nil {
			t.Fatal(err)
		}
		if img, err := png.Decode(bytes.NewReader(buf.Bytes())); err != nil || img.Bounds().Dx() != W {
			t.Fatalf("%s: %v", name, err)
		}
		if dir := os.Getenv("CARD_OUT"); dir != "" {
			os.WriteFile(dir+"/cooked-"+name+".png", buf.Bytes(), 0o644)
		}
	}
}
