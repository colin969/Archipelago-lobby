package main

import (
	"encoding/json"
	"fmt"
)

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
	Cmd            MessageType    `json:"cmd"`
	Password       *string        `json:"password"`
	Game           string         `json:"game"`
	Name           string         `json:"name"`
	UUID           string         `json:"uuid"`
	Version        NetworkVersion `json:"version"`
	ItemsHandling  *int           `json:"items_handling"`
	Tags           []string       `json:"tags"`
	SlotData       bool           `json:"slot_data"`
	ReducedTraffic bool           `json:"reduced"`
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

type NetworkSlotArray struct {
	Name         string `json:"name"`
	Game         string `json:"game"`
	Type         int    `json:"type"`
	GroupMembers []int  `json:"group_members"`
}

func (ns *NetworkSlotArray) UnmarshalJSON(data []byte) error {
	var raw [4]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	name, ok := raw[0].(string)
	if !ok {
		return fmt.Errorf("NetworkSlot[0] name: expected string, got %T", raw[0])
	}

	game, ok := raw[1].(string)
	if !ok {
		return fmt.Errorf("NetworkSlot[1] game: expected string, got %T", raw[1])
	}

	slotType, ok := raw[2].(float64)
	if !ok {
		return fmt.Errorf("NetworkSlot[2] type: expected number, got %T", raw[2])
	}

	var groupMembers []int
	if raw[3] != nil {
		members, ok := raw[3].([]any)
		if !ok {
			return fmt.Errorf("NetworkSlot[3] group_members: expected array, got %T", raw[3])
		}
		groupMembers = make([]int, len(members))
		for i, m := range members {
			f, ok := m.(float64)
			if !ok {
				return fmt.Errorf("NetworkSlot[3][%d]: expected number, got %T", i, m)
			}
			groupMembers[i] = int(f)
		}
	}

	ns.Name = name
	ns.Game = game
	ns.Type = int(slotType)
	ns.GroupMembers = groupMembers
	return nil
}

type NetworkVersion struct {
	Class string `json:"class"`
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Build int    `json:"build"`
}
