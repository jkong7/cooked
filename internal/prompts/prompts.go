package prompts

import (
	"hash/fnv"
	"time"
)

type Prompt struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Tag  string `json:"tag"`
}

var All = []Prompt{
	{1, "5'10 is short.", "dating"},
	{2, "A CS degree is worthless in 2026.", "money"},
	{3, "Splitting the bill on a first date is a red flag.", "dating"},
	{4, "Your girlfriend's best friend is your real competition.", "dating"},
	{5, "Going to the gym is a personality now, and that's fine.", "life"},
	{6, "Living with your parents until 27 is the smartest financial move.", "money"},
	{7, "Texting back instantly is a turn-off.", "dating"},
	{8, "Blue-collar jobs beat a college degree.", "money"},
	{9, "If you use AI to write your texts, you're cheating.", "tech"},
	{10, "Pineapple on pizza is objectively good.", "food"},
	{11, "DoorDash is a tax on people who can't plan.", "money"},
	{12, "Fantasy football punishments should be legally binding.", "life"},
	{13, "Group chats are where friendships go to die.", "life"},
	{14, "Anyone who posts their gym PRs is insecure.", "life"},
	{15, "Being a 'performative male' is still better than being boring.", "dating"},
	{16, "Leetcode is a scam and everyone knows it.", "tech"},
	{17, "You should never date someone from your friend group.", "dating"},
	{18, "Tipping culture has gone too far.", "money"},
	{19, "Remote work made everyone lazier.", "money"},
	{20, "Showering at night is superior.", "life"},
	{21, "Your credit score says more about you than your resume.", "money"},
	{22, "Crypto bros were right all along.", "money"},
	{23, "Posting your relationship online is a jinx.", "dating"},
	{24, "Gen Z has the worst work ethic of any generation.", "life"},
	{25, "Airpods in during a conversation is disrespectful.", "life"},
	{26, "Scrolling reels between Claude prompts is real work.", "tech"},
	{27, "A man should pay on the first date. Always.", "dating"},
	{28, "Brunch is overrated.", "food"},
	{29, "Being funny beats being hot.", "dating"},
	{30, "College is a four-year vacation you pay for.", "money"},
}

const Window = 10 * time.Minute

func ByID(id int) (Prompt, bool) {
	for _, p := range All {
		if p.ID == id {
			return p, true
		}
	}
	return Prompt{}, false
}

func Featured(now time.Time) Prompt {
	slot := now.Unix() / int64(Window.Seconds())
	h := fnv.New32a()
	h.Write([]byte{byte(slot), byte(slot >> 8), byte(slot >> 16), byte(slot >> 24)})
	return All[int(h.Sum32())%len(All)]
}

func NextRotation(now time.Time) time.Time {
	w := int64(Window.Seconds())
	return time.Unix((now.Unix()/w+1)*w, 0)
}
