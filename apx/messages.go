package main

const (
	MessageTypeConnect        MessageType = "Connect"
	MessageTypeConnectUpdate  MessageType = "ConnectUpdate"
	MessageTypeBounce         MessageType = "Bounce"
	MessageTypeGetDataPackage MessageType = "GetDataPackage"
	MessageTypeDataPackage    MessageType = "DataPackage"
)

const (
	PermissionDisabled    Permission = 0b000
	PermissionEnabled     Permission = 0b001
	PermissionGoal        Permission = 0b010
	PermissionAuto        Permission = 0b110
	PermissionAutoEnabled Permission = 0b111
)

type RoomInfoMessage struct {
	Cmd                  MessageType           `json:"cmd"`
	Version              NetworkVersion        `json:"version"`
	GeneratorVersion     NetworkVersion        `json:"generator_version"`
	Tags                 []string              `json:"tags"`
	Password             bool                  `json:"password"`
	Permissions          map[string]Permission `json:"permissions"`
	HintCost             int                   `json:"hint_cost"`
	LocationCheckPoints  int                   `json:"location_check_points"`
	Games                []string              `json:"games"`
	DatapackageChecksums map[string]string     `json:"datapackage_checksums"`
	SeedName             string                `json:"seed_name"`
	Time                 float64               `json:"time"`
}

type ConnectMessage struct {
	Cmd           MessageType    `json:"cmd"`
	Password      *string        `json:"password"`
	Game          string         `json:"game"`
	Name          string         `json:"name"`
	UUID          string         `json:"uuid"`
	Version       NetworkVersion `json:"version"`
	ItemsHandling *int           `json:"items_handling"`
	Tags          []string       `json:"tags"`
	SlotData      bool           `json:"slot_data"`
}

type ConnectionRefusedMessage struct {
	Cmd    MessageType `json:"cmd"`
	Errors []string    `json:"errors"`
}

type ConnectUpdateMessage struct {
	Cmd           MessageType `json:"cmd"`
	ItemsHandling *int        `json:"items_handling"`
	Tags          []string    `json:"tags"`
}

type ConnectedMessage struct {
	Team             int                 `json:"team"`
	Slot             int                 `json:"slot"`
	Players          []NetworkPlayer     `json:"players"`
	MissingLocations []int64             `json:"missing_locations"`
	CheckedLocations []int64             `json:"checked_locations"`
	SlotData         map[string]any      `json:"slot_data,omitempty"`
	SlotInfo         map[int]NetworkSlot `json:"slot_info"`
	HintPoints       int                 `json:"hint_points"`
}

type BounceMessage struct {
	Cmd   MessageType    `json:"cmd"`
	Games []string       `json:"games"`
	Slots []int          `json:"slots"`
	Tags  []string       `json:"tags"`
	Data  map[string]any `json:"data"`
}

type BounceDataDeathlink struct {
	Time   float64 `json:"time"`
	Source string  `json:"source"`
	Cause  *string `json:"cause,omitempty"`
}

type NetworkPlayer struct {
	Team  int    `json:"team"`
	Slot  int    `json:"slot"`
	Alias string `json:"alias"`
	Name  string `json:"name"`
}

type NetworkSlot struct {
	Name         string `json:"name"`
	Game         string `json:"game"`
	Type         int    `json:"type"`
	GroupMembers []int  `json:"group_members"`
}

type NetworkVersion struct {
	Class string `json:"class"`
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Build int    `json:"build"`
}
