package model

import "time"

type PROptions struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	BaseBranch string `json:"base_branch"`
	HeadBranch string `json:"head_branch"`
	Draft      bool   `json:"draft"`
}

type PRSplit struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Commits       []string `json:"commits"`
	StackPosition int      `json:"stack_position"`
}

type PRStatus struct {
	Number    int      `json:"number"`
	State     string   `json:"state"`
	CIStatus  string   `json:"ci_status"`
	CIDetails []Check  `json:"ci_details"`
	Reviews   []Review `json:"reviews"`
	Mergeable bool     `json:"mergeable"`
	URL       string   `json:"url"`
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Review struct {
	Author string `json:"author"`
	State  string `json:"state"`
	Body   string `json:"body"`
}

type PRActivity struct {
	Comments      []ReviewComment `json:"comments"`
	Reviews       []Review        `json:"reviews"`
	CITransitions []CITransition  `json:"ci_transitions"`
}

type ReviewComment struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}

type CITransition struct {
	CheckName string    `json:"check_name"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Time      time.Time `json:"time"`
}
