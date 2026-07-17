package twitterdigest

type Config struct {
	MinEngagement int      `json:"minEngagement"`
	MaxPerAuthor  int      `json:"maxPerAuthor"`
	Topics        []Topic  `json:"topics"`
	Source        string   `json:"source"`
	ListID        string   `json:"listId"`
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	DeliverTo     []string `json:"deliverTo"`
	EmailFrom     string   `json:"emailFrom"`
	EmailTo       []string `json:"emailTo"`
	EmailSubject  string   `json:"emailSubject"`
	JudgeModel    string   `json:"judgeModel"`   // optional; empty = judge with Model
	ReviseBudget  int      `json:"reviseBudget"` // max revision attempts per language when faithfulness fails; 0 = loop off
}

// Topic: one digest section
type Topic struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (c Config) judgeModel() string {
	if c.JudgeModel != "" {
		return c.JudgeModel
	}
	return c.Model
}
