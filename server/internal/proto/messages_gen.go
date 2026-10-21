// 由 tools/protogen 根据 proto/messages.json 生成，请勿手改。

package proto

import (
	"encoding/json"
	"fmt"
)

type CharSummary struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Sprite   int    `json:"sprite"`
	Face     int    `json:"face"`
	Level    int    `json:"level"`
	HP       int    `json:"hp"`
	Atk      int    `json:"atk"`
	Def      int    `json:"def"`
	Agi      int    `json:"agi"`
	Charm    int    `json:"charm"`
	DP       int    `json:"dp"`
	Attrs    []int  `json:"attrs"`
	Logins   int    `json:"logins"`
	Hometown int    `json:"hometown"`
}

type LineInfo struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Online int    `json:"online"`
}

type ServerGroup struct {
	Name  string     `json:"name"`
	Lines []LineInfo `json:"lines"`
}

type ObjInfo struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Sprite int    `json:"sprite"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Dir    int    `json:"dir"`
	Level  int    `json:"level"`
	Leader bool   `json:"leader"`
	Battle int    `json:"battle"`
	Help   bool   `json:"help"`
	Action int    `json:"action"`
	Family string `json:"family"`
}

type BattleFighter struct {
	Slot       int    `json:"slot"`
	Name       string `json:"name"`
	Sprite     int    `json:"sprite"`
	Level      int    `json:"level"`
	HP         int    `json:"hp"`
	MaxHp      int    `json:"maxHp"`
	Mp         int    `json:"mp"`
	MaxMp      int    `json:"maxMp"`
	RideName   string `json:"rideName"`
	RideLevel  int    `json:"rideLevel"`
	RideHp     int    `json:"rideHp"`
	RideMaxHp  int    `json:"rideMaxHp"`
	FallSprite int    `json:"fallSprite"`
}

type InvItem struct {
	Slot        int    `json:"slot"`
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Image       int    `json:"image"`
	Memo        string `json:"memo"`
	Level       int    `json:"level"`
	Pile        int    `json:"pile"`
	Type        int    `json:"type"`
	Field       int    `json:"field"`
	Target      int    `json:"target"`
	Usable      bool   `json:"usable"`
	Equip       int    `json:"equip"`
	Params      string `json:"params"`
	Damage      string `json:"damage"`
	Magic       string `json:"magic"`
	MagicMemo   string `json:"magicMemo"`
	MagicMp     int    `json:"magicMp"`
	MagicField  int    `json:"magicField"`
	MagicTarget int    `json:"magicTarget"`
	MagicDead   bool   `json:"magicDead"`
	MagicFunc   string `json:"magicFunc"`
	MagicOption string `json:"magicOption"`
	Flags       int    `json:"flags"`
	Alch        string `json:"alch"`
	Merged      bool   `json:"merged"`
	Jigsaw      string `json:"jigsaw"`
}

type BattleEvent struct {
	Actor     int    `json:"actor"`
	Kind      string `json:"kind"`
	Target    int    `json:"target"`
	Result    string `json:"result"`
	Damage    int    `json:"damage"`
	HP        int    `json:"hp"`
	Dead      bool   `json:"dead"`
	OK        bool   `json:"ok"`
	Skill     string `json:"skill"`
	Status    int    `json:"status"`
	Guardian  int    `json:"guardian"`
	Combo     bool   `json:"combo"`
	ActorHp   int    `json:"actorHp"`
	MpDamage  int    `json:"mpDamage"`
	Guarded   bool   `json:"guarded"`
	PetDamage int    `json:"petDamage"`
	RideHp    int    `json:"rideHp"`
	Fall      bool   `json:"fall"`
	Effect    int    `json:"effect"`
	ActorMp   int    `json:"actorMp"`
	Name      string `json:"name"`
	Sprite    int    `json:"sprite"`
	Level     int    `json:"level"`
	MaxHp     int    `json:"maxHp"`
	Att       string `json:"att"`
}

type ShopItem struct {
	Name   string `json:"name"`
	Level  int    `json:"level"`
	Price  int    `json:"price"`
	Image  int    `json:"image"`
	Info   string `json:"info"`
	Sealed bool   `json:"sealed"`
	Slot   int    `json:"slot"`
	Pile   int    `json:"pile"`
	Fame   int    `json:"fame"`
}

type PetSkill struct {
	Slot   int    `json:"slot"`
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Memo   string `json:"memo"`
	Field  int    `json:"field"`
	Target int    `json:"target"`
	Func   string `json:"func"`
	Option string `json:"option"`
}

type PetInfo struct {
	Slot      int        `json:"slot"`
	Name      string     `json:"name"`
	FreeName  string     `json:"freeName"`
	Sprite    int        `json:"sprite"`
	Level     int        `json:"level"`
	Exp       int        `json:"exp"`
	NextExp   int        `json:"nextExp"`
	HP        int        `json:"hp"`
	MaxHp     int        `json:"maxHp"`
	Atk       int        `json:"atk"`
	Def       int        `json:"def"`
	Agi       int        `json:"agi"`
	Loyalty   int        `json:"loyalty"`
	Attrs     []int      `json:"attrs"`
	Trans     int        `json:"trans"`
	MaxSkill  int        `json:"maxSkill"`
	Skills    []PetSkill `json:"skills"`
	State     string     `json:"state"`
	CanRename bool       `json:"canRename"`
	Rideable  bool       `json:"rideable"`
	Items     []InvItem  `json:"items"`
	Guard     bool       `json:"guard"`
	Ext       PetExt     `json:"ext"`
}

type PetExt struct {
	Rank     int   `json:"rank"`
	GetLevel int   `json:"getLevel"`
	EnemyID  int   `json:"enemyId"`
	Alloc    []int `json:"alloc"`
	Std      []int `json:"std"`
	Grow     []int `json:"grow"`
	Real     []int `json:"real"`
	Init     []int `json:"init"`
	Resist   []int `json:"resist"`
	Critical int   `json:"critical"`
	Counter  int   `json:"counter"`
	Rare     int   `json:"rare"`
	Vai      int   `json:"vai"`
	Magic    []int `json:"magic"`
}

type GmItem struct {
	Cmd      string   `json:"cmd"`
	Name     string   `json:"name"`
	Group    string   `json:"group"`
	Args     []string `json:"args"`
	Kinds    []string `json:"kinds"`
	Defaults []string `json:"defaults"`
	Hint     string   `json:"hint"`
}

type GmEntry struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Info  string `json:"info"`
	Image int    `json:"image"`
}

type AddressEntry struct {
	Slot   int    `json:"slot"`
	Used   bool   `json:"used"`
	Name   string `json:"name"`
	Level  int    `json:"level"`
	DP     int    `json:"dp"`
	Online int    `json:"online"`
	Face   int    `json:"face"`
	Trans  int    `json:"trans"`
}

type PetResult struct {
	Slot    int    `json:"slot"`
	Name    string `json:"name"`
	Exp     int    `json:"exp"`
	LevelUp int    `json:"levelUp"`
	Level   int    `json:"level"`
}

type SetFlags struct {
	Flags int `json:"flags"`
}

func (*SetFlags) MsgType() string { return "SetFlags" }

type CardExchange struct {
}

func (*CardExchange) MsgType() string { return "CardExchange" }

type CardDelete struct {
	Index int `json:"index"`
}

func (*CardDelete) MsgType() string { return "CardDelete" }

type MailSend struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

func (*MailSend) MsgType() string { return "MailSend" }

type PetMail struct {
	Index int    `json:"index"`
	Pet   int    `json:"pet"`
	Item  int    `json:"item"`
	Text  string `json:"text"`
}

func (*PetMail) MsgType() string { return "PetMail" }

type Family struct {
	Text string `json:"text"`
}

func (*Family) MsgType() string { return "Family" }

type Action struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Action int `json:"action"`
}

func (*Action) MsgType() string { return "Action" }

type Trade struct {
	Cmd    string `json:"cmd"`
	Slot   int    `json:"slot"`
	Amount int    `json:"amount"`
}

func (*Trade) MsgType() string { return "Trade" }

type Party struct {
	Request int `json:"request"`
}

func (*Party) MsgType() string { return "Party" }

type PartyKick struct {
	Slot int `json:"slot"`
}

func (*PartyKick) MsgType() string { return "PartyKick" }

type BattleHelp struct {
	On bool `json:"on"`
}

func (*BattleHelp) MsgType() string { return "BattleHelp" }

type JoinBattle struct {
}

func (*JoinBattle) MsgType() string { return "JoinBattle" }

type Duel struct {
}

func (*Duel) MsgType() string { return "Duel" }

type Register struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

func (*Register) MsgType() string { return "Register" }

type Login struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

func (*Login) MsgType() string { return "Login" }

type SelectLine struct {
	ID int `json:"id"`
}

func (*SelectLine) MsgType() string { return "SelectLine" }

type CharList struct {
}

func (*CharList) MsgType() string { return "CharList" }

type CharCreate struct {
	Name     string `json:"name"`
	Sprite   int    `json:"sprite"`
	Face     int    `json:"face"`
	Vital    int    `json:"vital"`
	Str      int    `json:"str"`
	Tgh      int    `json:"tgh"`
	Dex      int    `json:"dex"`
	Earth    int    `json:"earth"`
	Water    int    `json:"water"`
	Fire     int    `json:"fire"`
	Wind     int    `json:"wind"`
	Hometown int    `json:"hometown"`
}

func (*CharCreate) MsgType() string { return "CharCreate" }

type CharDelete struct {
	ID int `json:"id"`
}

func (*CharDelete) MsgType() string { return "CharDelete" }

type CharEnter struct {
	ID int `json:"id"`
}

func (*CharEnter) MsgType() string { return "CharEnter" }

type MapReq struct {
	Floor int `json:"floor"`
	X1    int `json:"x1"`
	Y1    int `json:"y1"`
	X2    int `json:"x2"`
	Y2    int `json:"y2"`
}

func (*MapReq) MsgType() string { return "MapReq" }

type Move struct {
	X   int `json:"x"`
	Y   int `json:"y"`
	Dir int `json:"dir"`
}

func (*Move) MsgType() string { return "Move" }

type Turn struct {
	Dir int `json:"dir"`
}

func (*Turn) MsgType() string { return "Turn" }

type Talk struct {
	ID int `json:"id"`
}

func (*Talk) MsgType() string { return "Talk" }

type Chat struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
	Color   int    `json:"color"`
	Area    int    `json:"area"`
}

func (*Chat) MsgType() string { return "Chat" }

type ReturnHome struct {
}

func (*ReturnHome) MsgType() string { return "ReturnHome" }

type BattleCommand struct {
	Kind   string `json:"kind"`
	Target int    `json:"target"`
	Item   int    `json:"item"`
}

func (*BattleCommand) MsgType() string { return "BattleCommand" }

type BattlePetCommand struct {
	Skill  int `json:"skill"`
	Target int `json:"target"`
}

func (*BattlePetCommand) MsgType() string { return "BattlePetCommand" }

type PetState struct {
	Slot  int    `json:"slot"`
	State string `json:"state"`
}

func (*PetState) MsgType() string { return "PetState" }

type PetRename struct {
	Slot int    `json:"slot"`
	Name string `json:"name"`
}

func (*PetRename) MsgType() string { return "PetRename" }

type PetDrop struct {
	Slot int `json:"slot"`
}

func (*PetDrop) MsgType() string { return "PetDrop" }

type PetRide struct {
	Slot int `json:"slot"`
}

func (*PetRide) MsgType() string { return "PetRide" }

type ItemMove struct {
	From int `json:"from"`
	To   int `json:"to"`
}

func (*ItemMove) MsgType() string { return "ItemMove" }

type PetItemEquip struct {
	Pet  int `json:"pet"`
	From int `json:"from"`
	To   int `json:"to"`
}

func (*PetItemEquip) MsgType() string { return "PetItemEquip" }

type PetSkillUse struct {
	Pet   int    `json:"pet"`
	Skill int    `json:"skill"`
	Data  string `json:"data"`
}

func (*PetSkillUse) MsgType() string { return "PetSkillUse" }

type ItemUse struct {
	Slot   int `json:"slot"`
	Target int `json:"target"`
}

func (*ItemUse) MsgType() string { return "ItemUse" }

type MagicUse struct {
	Slot   int `json:"slot"`
	Target int `json:"target"`
}

func (*MagicUse) MsgType() string { return "MagicUse" }

type ItemDrop struct {
	Slot int `json:"slot"`
}

func (*ItemDrop) MsgType() string { return "ItemDrop" }

type GoldDrop struct {
	Amount int `json:"amount"`
}

func (*GoldDrop) MsgType() string { return "GoldDrop" }

type PickUp struct {
	Dir int `json:"dir"`
}

func (*PickUp) MsgType() string { return "PickUp" }

type StatUp struct {
	Stat int `json:"stat"`
}

func (*StatUp) MsgType() string { return "StatUp" }

type NpcWindowReply struct {
	ID     int    `json:"id"`
	Seq    int    `json:"seq"`
	Button int    `json:"button"`
	Data   string `json:"data"`
}

func (*NpcWindowReply) MsgType() string { return "NpcWindowReply" }

type Ping struct {
	Seq int `json:"seq"`
}

func (*Ping) MsgType() string { return "Ping" }

type GmCommand struct {
	Text string `json:"text"`
}

func (*GmCommand) MsgType() string { return "GmCommand" }

type GmListReq struct {
	Kind string `json:"kind"`
}

func (*GmListReq) MsgType() string { return "GmListReq" }

type GmDetailReq struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

func (*GmDetailReq) MsgType() string { return "GmDetailReq" }

type Logout struct {
}

func (*Logout) MsgType() string { return "Logout" }

type Flags struct {
	Flags int `json:"flags"`
}

func (*Flags) MsgType() string { return "Flags" }

type GmMenu struct {
	Items []GmItem `json:"items"`
}

func (*GmMenu) MsgType() string { return "GmMenu" }

type GmList struct {
	Kind    string    `json:"kind"`
	Entries []GmEntry `json:"entries"`
}

func (*GmList) MsgType() string { return "GmList" }

type GmDetail struct {
	Kind  string   `json:"kind"`
	ID    int      `json:"id"`
	Image int      `json:"image"`
	Lines []string `json:"lines"`
}

func (*GmDetail) MsgType() string { return "GmDetail" }

type DuelResult struct {
	Delta int `json:"delta"`
	DP    int `json:"dp"`
}

func (*DuelResult) MsgType() string { return "DuelResult" }

type FamilyMsg struct {
	Text string `json:"text"`
}

func (*FamilyMsg) MsgType() string { return "FamilyMsg" }

type FamilyStatus struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Duty    int    `json:"duty"`
	Channel int    `json:"channel"`
	Sprite  int    `json:"sprite"`
}

func (*FamilyStatus) MsgType() string { return "FamilyStatus" }

type ObjFamily struct {
	ID     int    `json:"id"`
	Family string `json:"family"`
}

func (*ObjFamily) MsgType() string { return "ObjFamily" }

type AddressBook struct {
	Entries []AddressEntry `json:"entries"`
}

func (*AddressBook) MsgType() string { return "AddressBook" }

type AddressUpdate struct {
	Entries []AddressEntry `json:"entries"`
}

func (*AddressUpdate) MsgType() string { return "AddressUpdate" }

type MailRecv struct {
	Index     int    `json:"index"`
	Date      string `json:"date"`
	Text      string `json:"text"`
	PetSprite int    `json:"petSprite"`
	PetLevel  int    `json:"petLevel"`
	PetName   string `json:"petName"`
	ItemImage int    `json:"itemImage"`
}

func (*MailRecv) MsgType() string { return "MailRecv" }

type ObjAction struct {
	ID     int `json:"id"`
	Action int `json:"action"`
}

func (*ObjAction) MsgType() string { return "ObjAction" }

type TradeOpen struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (*TradeOpen) MsgType() string { return "TradeOpen" }

type TradeItem struct {
	Slot  int       `json:"slot"`
	Count int       `json:"count"`
	Items []InvItem `json:"items"`
}

func (*TradeItem) MsgType() string { return "TradeItem" }

type TradeGold struct {
	Amount int `json:"amount"`
}

func (*TradeGold) MsgType() string { return "TradeGold" }

type TradePet struct {
	Pets []PetInfo `json:"pets"`
}

func (*TradePet) MsgType() string { return "TradePet" }

type TradeState struct {
	State string `json:"state"`
}

func (*TradeState) MsgType() string { return "TradeState" }

type TradeClose struct {
}

func (*TradeClose) MsgType() string { return "TradeClose" }

type PartyResult struct {
	Request int  `json:"request"`
	OK      bool `json:"ok"`
}

func (*PartyResult) MsgType() string { return "PartyResult" }

type PartyMember struct {
	Slot  int    `json:"slot"`
	Used  bool   `json:"used"`
	ID    int    `json:"id"`
	Level int    `json:"level"`
	MaxHp int    `json:"maxHp"`
	HP    int    `json:"hp"`
	Mp    int    `json:"mp"`
	Name  string `json:"name"`
}

func (*PartyMember) MsgType() string { return "PartyMember" }

type ObjMark struct {
	ID     int  `json:"id"`
	Leader bool `json:"leader"`
	Battle int  `json:"battle"`
	Help   bool `json:"help"`
}

func (*ObjMark) MsgType() string { return "ObjMark" }

type HelpFlag struct {
	On bool `json:"on"`
}

func (*HelpFlag) MsgType() string { return "HelpFlag" }

type BattleJoin struct {
	Fighters []BattleFighter `json:"fighters"`
}

func (*BattleJoin) MsgType() string { return "BattleJoin" }

type Hello struct {
	Version    string `json:"version"`
	ServerTime int    `json:"serverTime"`
}

func (*Hello) MsgType() string { return "Hello" }

type PetSkillResult struct {
	OK    bool `json:"ok"`
	Pet   int  `json:"pet"`
	Skill int  `json:"skill"`
}

func (*PetSkillResult) MsgType() string { return "PetSkillResult" }

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (*Error) MsgType() string { return "Error" }

type AuthResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (*AuthResult) MsgType() string { return "AuthResult" }

type ServerList struct {
	Groups []ServerGroup `json:"groups"`
}

func (*ServerList) MsgType() string { return "ServerList" }

type LineSelected struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (*LineSelected) MsgType() string { return "LineSelected" }

type CharListResult struct {
	Chars    []CharSummary `json:"chars"`
	MaxChars int           `json:"maxChars"`
}

func (*CharListResult) MsgType() string { return "CharListResult" }

type CharCreateResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	ID      int    `json:"id"`
}

func (*CharCreateResult) MsgType() string { return "CharCreateResult" }

type Self struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Sprite int    `json:"sprite"`
}

func (*Self) MsgType() string { return "Self" }

type Status struct {
	Name     string `json:"name"`
	Face     int    `json:"face"`
	Level    int    `json:"level"`
	Exp      int    `json:"exp"`
	NextExp  int    `json:"nextExp"`
	HP       int    `json:"hp"`
	MaxHp    int    `json:"maxHp"`
	Mp       int    `json:"mp"`
	MaxMp    int    `json:"maxMp"`
	Atk      int    `json:"atk"`
	Def      int    `json:"def"`
	Agi      int    `json:"agi"`
	Charm    int    `json:"charm"`
	DP       int    `json:"dp"`
	Vital    int    `json:"vital"`
	Str      int    `json:"str"`
	Tgh      int    `json:"tgh"`
	Dex      int    `json:"dex"`
	Attrs    []int  `json:"attrs"`
	Hometown int    `json:"hometown"`
	Trans    int    `json:"trans"`
	Points   int    `json:"points"`
}

func (*Status) MsgType() string { return "Status" }

type EnterFloor struct {
	Floor   int    `json:"floor"`
	Name    string `json:"name"`
	Palette int    `json:"palette"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Dir     int    `json:"dir"`
}

func (*EnterFloor) MsgType() string { return "EnterFloor" }

type MapData struct {
	Floor int    `json:"floor"`
	X1    int    `json:"x1"`
	Y1    int    `json:"y1"`
	X2    int    `json:"x2"`
	Y2    int    `json:"y2"`
	Tiles []int  `json:"tiles"`
	Objs  []int  `json:"objs"`
	Walk  string `json:"walk"`
}

func (*MapData) MsgType() string { return "MapData" }

type PosCorrect struct {
	X   int `json:"x"`
	Y   int `json:"y"`
	Dir int `json:"dir"`
}

func (*PosCorrect) MsgType() string { return "PosCorrect" }

type ObjAppear struct {
	Objs []ObjInfo `json:"objs"`
}

func (*ObjAppear) MsgType() string { return "ObjAppear" }

type ObjDisappear struct {
	IDs []int `json:"ids"`
}

func (*ObjDisappear) MsgType() string { return "ObjDisappear" }

type ObjMove struct {
	ID  int `json:"id"`
	X   int `json:"x"`
	Y   int `json:"y"`
	Dir int `json:"dir"`
}

func (*ObjMove) MsgType() string { return "ObjMove" }

type ObjTurn struct {
	ID  int `json:"id"`
	Dir int `json:"dir"`
}

func (*ObjTurn) MsgType() string { return "ObjTurn" }

type ObjSprite struct {
	ID     int `json:"id"`
	Sprite int `json:"sprite"`
}

func (*ObjSprite) MsgType() string { return "ObjSprite" }

type NpcTalk struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Text   string `json:"text"`
	Window bool   `json:"window"`
}

func (*NpcTalk) MsgType() string { return "NpcTalk" }

type NpcWindow struct {
	ID      int    `json:"id"`
	Kind    string `json:"kind"`
	Buttons int    `json:"buttons"`
	Seq     int    `json:"seq"`
	Text    string `json:"text"`
}

func (*NpcWindow) MsgType() string { return "NpcWindow" }

type ShopWindow struct {
	ID       int        `json:"id"`
	Seq      int        `json:"seq"`
	Mode     string     `json:"mode"`
	Title    string     `json:"title"`
	Msg      string     `json:"msg"`
	Main     string     `json:"main"`
	What     string     `json:"what"`
	Level    string     `json:"level"`
	Realy    string     `json:"realy"`
	Full     string     `json:"full"`
	Stone    string     `json:"stone"`
	SellOnly bool       `json:"sellOnly"`
	MaxGold  int        `json:"maxGold"`
	Fame     int        `json:"fame"`
	Rest     int        `json:"rest"`
	Items    []ShopItem `json:"items"`
}

func (*ShopWindow) MsgType() string { return "ShopWindow" }

type ChatMsg struct {
	Channel string `json:"channel"`
	FromID  int    `json:"fromId"`
	From    string `json:"from"`
	Text    string `json:"text"`
	Color   int    `json:"color"`
}

func (*ChatMsg) MsgType() string { return "ChatMsg" }

type Pong struct {
	Seq        int `json:"seq"`
	ServerTime int `json:"serverTime"`
}

func (*Pong) MsgType() string { return "Pong" }

type BattleStart struct {
	Field    int             `json:"field"`
	MySlot   int             `json:"mySlot"`
	TurnTime int             `json:"turnTime"`
	Surprise int             `json:"surprise"`
	Fighters []BattleFighter `json:"fighters"`
	Pvp      bool            `json:"pvp"`
	Event    bool            `json:"event"`
}

func (*BattleStart) MsgType() string { return "BattleStart" }

type BattleTurn struct {
	Turn   int           `json:"turn"`
	Events []BattleEvent `json:"events"`
}

func (*BattleTurn) MsgType() string { return "BattleTurn" }

type BattleEnd struct {
	Result  string      `json:"result"`
	Exp     int         `json:"exp"`
	LevelUp int         `json:"levelUp"`
	Level   int         `json:"level"`
	Items   []string    `json:"items"`
	Pets    []PetResult `json:"pets"`
}

func (*BattleEnd) MsgType() string { return "BattleEnd" }

type Pets struct {
	Battle int       `json:"battle"`
	Ride   int       `json:"ride"`
	Pets   []PetInfo `json:"pets"`
}

func (*Pets) MsgType() string { return "Pets" }

type Inventory struct {
	Gold  int       `json:"gold"`
	Items []InvItem `json:"items"`
}

func (*Inventory) MsgType() string { return "Inventory" }

// Message 是所有消息共有的接口。
type Message interface{ MsgType() string }

// DecodeClient 解析客户端发来的消息；未定义的类型返回错误。
func DecodeClient(data []byte) (Message, error) {
	var head struct {
		T string `json:"t"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	var m Message
	switch head.T {
	case "SetFlags":
		m = &SetFlags{}
	case "CardExchange":
		m = &CardExchange{}
	case "CardDelete":
		m = &CardDelete{}
	case "MailSend":
		m = &MailSend{}
	case "PetMail":
		m = &PetMail{}
	case "Family":
		m = &Family{}
	case "Action":
		m = &Action{}
	case "Trade":
		m = &Trade{}
	case "Party":
		m = &Party{}
	case "PartyKick":
		m = &PartyKick{}
	case "BattleHelp":
		m = &BattleHelp{}
	case "JoinBattle":
		m = &JoinBattle{}
	case "Duel":
		m = &Duel{}
	case "Register":
		m = &Register{}
	case "Login":
		m = &Login{}
	case "SelectLine":
		m = &SelectLine{}
	case "CharList":
		m = &CharList{}
	case "CharCreate":
		m = &CharCreate{}
	case "CharDelete":
		m = &CharDelete{}
	case "CharEnter":
		m = &CharEnter{}
	case "MapReq":
		m = &MapReq{}
	case "Move":
		m = &Move{}
	case "Turn":
		m = &Turn{}
	case "Talk":
		m = &Talk{}
	case "Chat":
		m = &Chat{}
	case "ReturnHome":
		m = &ReturnHome{}
	case "BattleCommand":
		m = &BattleCommand{}
	case "BattlePetCommand":
		m = &BattlePetCommand{}
	case "PetState":
		m = &PetState{}
	case "PetRename":
		m = &PetRename{}
	case "PetDrop":
		m = &PetDrop{}
	case "PetRide":
		m = &PetRide{}
	case "ItemMove":
		m = &ItemMove{}
	case "PetItemEquip":
		m = &PetItemEquip{}
	case "PetSkillUse":
		m = &PetSkillUse{}
	case "ItemUse":
		m = &ItemUse{}
	case "MagicUse":
		m = &MagicUse{}
	case "ItemDrop":
		m = &ItemDrop{}
	case "GoldDrop":
		m = &GoldDrop{}
	case "PickUp":
		m = &PickUp{}
	case "StatUp":
		m = &StatUp{}
	case "NpcWindowReply":
		m = &NpcWindowReply{}
	case "Ping":
		m = &Ping{}
	case "GmCommand":
		m = &GmCommand{}
	case "GmListReq":
		m = &GmListReq{}
	case "GmDetailReq":
		m = &GmDetailReq{}
	case "Logout":
		m = &Logout{}
	default:
		return nil, fmt.Errorf("未知消息类型 %q", head.T)
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Encode 序列化消息并加上类型字段 t。
func Encode(m Message) ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	head := fmt.Sprintf(`{"t":%q`, m.MsgType())
	if len(body) > 2 {
		return append([]byte(head+","), body[1:]...), nil
	}
	return []byte(head + "}"), nil
}
