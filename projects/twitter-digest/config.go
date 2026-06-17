package twitterdigest

type Config struct {
	MinEngagement int      `json:"minEngagement"`
	Topics        []string `json:"topics"`
	Source        string   `json:"source"`
	Model         string   `json:"model"`
}
