package store

import (
	"encoding/json"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Board is a whiteboard.
type Board struct {
	ID         int64           `json:"id"`
	UserID     int64           `json:"user_id"`
	Title      string          `json:"title"`
	About      string          `json:"about"`
	Template   string          `json:"template"`
	Background string          `json:"background"`
	ShareKey   string          `json:"share_key,omitempty"`
	ShareMode  string          `json:"share_mode"`
	TimerEnds  string          `json:"timer_ends"`
	TimerSecs  int             `json:"timer_secs"`
	Vote       json.RawMessage `json:"vote"`
	Private    json.RawMessage `json:"private"`
	Version    int64           `json:"version"`
	Source     string          `json:"source,omitempty"`
	ExtID      string          `json:"ext_id,omitempty"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`

	// filled in for the person asking
	OwnerName string `json:"owner_name"`
	Role      string `json:"role,omitempty"` // owner | editor | viewer
	Starred   bool   `json:"starred"`
	OpenedAt  string `json:"opened_at,omitempty"`
	Objects   int    `json:"objects_n"`
	Members   int    `json:"members_n"`
	FolderID  int64  `json:"folder_id"`
}

// Vote is a board's voting round.
type Vote struct {
	Round     int    `json:"round"`
	PerPerson int    `json:"per_person"`
	Open      bool   `json:"open"`
	Done      bool   `json:"done"` // ended: the results show
	Hide      bool   `json:"hide"` // counts hidden until the round ends
	StartedAt string `json:"started_at,omitempty"`
	StartedBy string `json:"started_by,omitempty"`
}

// Private is a board's private mode: notes made while it is on stay with
// whoever made them until someone reveals them.
type Private struct {
	On        bool   `json:"on"`
	By        string `json:"by,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
}

// Comment is one comment; a thread is a first comment and its replies.
type Comment struct {
	ID        int64   `json:"id"`
	BoardID   int64   `json:"board_id"`
	ParentID  *int64  `json:"parent_id"`
	ObjectID  string  `json:"object_id"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	UserID    *int64  `json:"user_id"`
	Author    string  `json:"author"`
	Voter     string  `json:"voter"`
	Body      string  `json:"body"`
	Mentions  []int64 `json:"mentions"`
	Resolved  bool    `json:"resolved"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// Version is the board as it was at one moment.
type Version struct {
	ID        int64  `json:"id"`
	BoardID   int64  `json:"board_id"`
	Name      string `json:"name"`
	Who       string `json:"who"`
	N         int    `json:"n"`
	CreatedAt string `json:"created_at"`
}

// Trashed is something deleted from a board, to put back.
type Trashed struct {
	ID        int64  `json:"id"`
	ObjectID  string `json:"object_id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	DeletedBy string `json:"deleted_by"`
	DeletedAt string `json:"deleted_at"`
}

// Member is a person on this box on a board.
type Member struct {
	BoardID int64  `json:"board_id"`
	UserID  int64  `json:"user_id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	AddedAt string `json:"added_at"`
}

// Object is one thing on a board.
type Object struct {
	ID        string          `json:"id"`
	BoardID   int64           `json:"-"`
	UserID    *int64          `json:"user_id"`
	Author    string          `json:"author"`
	Kind      string          `json:"kind"`
	X         float64         `json:"x"`
	Y         float64         `json:"y"`
	W         float64         `json:"w"`
	H         float64         `json:"h"`
	Z         float64         `json:"z"`
	Data      json.RawMessage `json:"data"`
	Source    string          `json:"source,omitempty"`
	UpdatedBy string          `json:"updated_by"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
	Hidden    bool            `json:"hidden,omitempty"` // private to whoever made it (private mode)
	HiddenBy  string          `json:"-"`
}

// Kinds are the things a board can hold.
var Kinds = []string{"sticky", "text", "shape", "line", "frame", "pen", "image", "sticker", "card", "embed", "icon"}

// Upload is a picture put on a board.
type Upload struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	BoardID   int64  `json:"board_id"`
	Name      string `json:"name"`
	Mime      string `json:"mime"`
	Bytes     int    `json:"bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Data      []byte `json:"-"`
	Thumb     []byte `json:"-"`
	CreatedAt string `json:"created_at"`
}

// Conn is one person's connection to a sibling Pocket app.
type Conn struct {
	Mode      string `json:"mode,omitempty"`
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Boards.
	Background  string `json:"background"`   // a new board's background: dots | grid | plain
	StickyColor string `json:"sticky_color"` // the first colour of a new sticky note
	Snap        bool   `json:"snap"`         // things snap to the grid while moving
	Cursors     bool   `json:"cursors"`      // show where the others are pointing
	WheelZoom   bool   `json:"wheel_zoom"`   // the mouse wheel zooms (else it moves the board; Ctrl zooms)

	// Other apps.
	NotesFolder string `json:"notes_folder"` // where boards go in Pocket Notes
	FilesFolder string `json:"files_folder"` // where pictures of boards go in Pocket Files
	ChatTo      string `json:"chat_to"`      // the Pocket Chat conversation boards are shared in
	AIHelp      bool   `json:"ai_help"`

	// Notifications.
	NotifyShared   bool   `json:"notify_shared"`   // someone put you on a board
	NotifyMentions bool   `json:"notify_mentions"` // someone @mentioned you in a comment
	NotifyComments bool   `json:"notify_comments"` // someone replied in a thread you are in
	NotifyURL      string `json:"notify_url"`
	NotifyFormat   string `json:"notify_format"` // apprise | ntfy | json

	// Each sibling Pocket app, by slug.
	Conns map[string]Conn `json:"conns"`

	// AI: Pocket AI through Core, or an OpenAI-compatible API with the
	// person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
