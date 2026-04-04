package foreman

// PaginatedMinersResponse is the paginated response from the Foreman miners endpoint.
type PaginatedMinersResponse struct {
	Limit   int     `json:"limit"`
	Offset  int     `json:"offset"`
	Total   int     `json:"total"`
	Results []Miner `json:"results"`
}

// Miner represents a miner returned by the Foreman API.
type Miner struct {
	ID          int            `json:"id"`
	Client      string         `json:"client"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        MinerType      `json:"type"`
	IP          string         `json:"ip"`
	APIPort     int            `json:"apiPort"`
	MAC         string         `json:"mac"`
	Serial      string         `json:"serial"`
	PSUSerial   *string        `json:"psuSerial"`
	Description string         `json:"description"`
	Pickaxe     int            `json:"pickaxe"`
	Created     string         `json:"created"`
	LastUpdated string         `json:"lastUpdated"`
	Seen        bool           `json:"seen"`
	Active      bool           `json:"active"`
	Status      string         `json:"status"`
	Location    *MinerLocation `json:"location"`
	Firmware    Firmware       `json:"firmware"`
	Network     Network        `json:"network"`
	State       string         `json:"state"`
	Tags        []Tag          `json:"tags"`
	Pools       []Pool         `json:"pools"`
}

// MinerType describes the hardware type of a miner.
type MinerType struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Firmware describes the firmware running on a miner.
type Firmware struct {
	Author  string `json:"author"`
	Version string `json:"version"`
}

// Network contains the miner's network details.
type Network struct {
	IP string `json:"ip"`
}

// Pool represents a pool configuration on a miner.
type Pool struct {
	URL      string `json:"url"`
	Worker   string `json:"worker"`
	Status   bool   `json:"status"`
	Enabled  bool   `json:"enabled"`
	Accepted int    `json:"accepted"`
	Rejected int    `json:"rejected"`
	Stale    int    `json:"stale"`
}

// Tag is a label attached to a miner.
type Tag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// MinerLocation is the miner's physical position within a rack.
type MinerLocation struct {
	RackID int `json:"rackId"`
	Row    int `json:"row"`
	Index  int `json:"index"`
}

// SiteMapGroup represents a Foreman site map group.
type SiteMapGroup struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	GroupID *int   `json:"groupId"` // parent group ID (nil if top-level)
	Row     int    `json:"row"`
	Column  int    `json:"column"`
}

// SiteMapRack represents a Foreman site map rack.
type SiteMapRack struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	GroupID *int   `json:"groupId"` // parent group ID
	Row     int    `json:"row"`
	Column  int    `json:"column"`
}
