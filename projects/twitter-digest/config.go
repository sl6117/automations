package twitterdigest

type Config struct {
	MinEngagement int      `json:"minEngagement"`
	MaxPerAuthor  int      `json:"maxPerAuthor"`
	Topics        []string `json:"topics"`
	Source        string   `json:"source"`
	ListID        string   `json:"listId"`
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	DeliverTo     []string `json:"deliverTo"`
	EmailFrom     string   `json:"emailFrom"`
	EmailTo       []string `json:"emailTo"`
	EmailSubject  string   `json:"emailSubject"`
}
