package opsreminder

import "time"

type Evidence struct {
	Ref  string    `json:"ref"`
	URL  string    `json:"url"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
}

type Candidate struct {
	ID           string     `json:"id"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	AssigneeKeys []string   `json:"assignee_keys"`
	Checked      bool       `json:"checked"`
	ThreadIDs    []string   `json:"thread_ids"`
	DoneReaction bool       `json:"done_reaction"`
	Evidence     []Evidence `json:"evidence"`
}

type Decision struct {
	CandidateID  string   `json:"candidate_id"`
	TaskTitle    string   `json:"task_title"`
	Status       string   `json:"status"`
	DueDate      *string  `json:"due_date"`
	AssigneeKeys []string `json:"assignee_keys"`
	Reason       string   `json:"reason"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type Verification struct {
	CandidateID  string   `json:"candidate_id"`
	TaskTitle    string   `json:"task_title"`
	Certainty    string   `json:"certainty"`
	Status       string   `json:"status"`
	DueDate      *string  `json:"due_date"`
	AssigneeKeys []string `json:"assignee_keys"`
	Reason       string   `json:"reason"`
	EvidenceRefs []string `json:"evidence_refs"`
}

func (v Verification) Decision() Decision {
	return Decision{CandidateID: v.CandidateID, TaskTitle: v.TaskTitle, Status: v.Status, DueDate: v.DueDate, AssigneeKeys: v.AssigneeKeys, Reason: v.Reason, EvidenceRefs: v.EvidenceRefs}
}

type UncertainItem struct {
	CandidateID string `json:"candidate_id"`
	Title       string `json:"title"`
	DueDate     string `json:"due_date"`
	NotionURL   string `json:"notion_url"`
	Reason      string `json:"reason"`
}

type MeetingFollowup struct {
	CandidateID string   `json:"candidate_id"`
	Title       string   `json:"title"`
	DueDate     string   `json:"due_date,omitempty"`
	NotionURL   string   `json:"notion_url"`
	ThreadURL   string   `json:"thread_url,omitempty"`
	UserIDs     []string `json:"discord_user_ids,omitempty"`
}

type PipelineTask struct {
	Owner           string `json:"owner"`
	Title           string `json:"title"`
	DueDate         string `json:"due_date,omitempty"`
	DefaultDeadline bool   `json:"default_deadline,omitempty"`
	SourceURL       string `json:"source_url"`
}

type Reminder struct {
	CandidateID  string   `json:"candidate_id"`
	Title        string   `json:"title"`
	DueDate      string   `json:"due_date"`
	NotionURL    string   `json:"notion_url"`
	ThreadURL    string   `json:"thread_url,omitempty"`
	UserIDs      []string `json:"discord_user_ids"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type Result struct {
	SourceMeetingDate string            `json:"source_meeting_date,omitempty"`
	NextMeetingDate   string            `json:"next_meeting_date,omitempty"`
	PlannedTasks      []PipelineTask    `json:"planned_tasks,omitempty"`
	CreatedTasks      int               `json:"created_tasks,omitempty"`
	CheckedTasks      int               `json:"checked_tasks,omitempty"`
	Collected         int               `json:"collected"`
	Eligible          int               `json:"eligible"`
	AlreadySent       int               `json:"already_sent"`
	Sent              int               `json:"sent"`
	DryRun            bool              `json:"dry_run"`
	TestMode          bool              `json:"test_mode"`
	Processed         int               `json:"processed"`
	Skipped           []string          `json:"skipped,omitempty"`
	Reminders         []Reminder        `json:"reminders,omitempty"`
	Uncertain         []UncertainItem   `json:"uncertain,omitempty"`
	Followups         []MeetingFollowup `json:"followups,omitempty"`
}
