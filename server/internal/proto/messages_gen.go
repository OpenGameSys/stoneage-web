// 由 tools/protogen 根据 proto/messages.json 生成，请勿手改。

package proto

import (
	"encoding/json"
	"fmt"
)

type CharSummary struct {
	ID       int    `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Sprite   int    `json:"sprite,omitempty"`
	Face     int    `json:"face,omitempty"`
	Level    int    `json:"level,omitempty"`
	HP       int    `json:"hp,omitempty"`
	Atk      int    `json:"atk,omitempty"`
	Def      int    `json:"def,omitempty"`
	Agi      int    `json:"agi,omitempty"`
	Charm    int    `json:"charm,omitempty"`
	DP       int    `json:"dp,omitempty"`
	Attrs    []int  `json:"attrs,omitempty"`
	Logins   int    `json:"logins,omitempty"`
	Hometown int    `json:"hometown,omitempty"`
}

type LineInfo struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Online int    `json:"online,omitempty"`
}

type ServerGroup struct {
	Name  string     `json:"name,omitempty"`
	Lines []LineInfo `json:"lines,omitempty"`
}

type ObjInfo struct {
	ID     int    `json:"id,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name,omitempty"`
	Sprite int    `json:"sprite,omitempty"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	Dir    int    `json:"dir,omitempty"`
	Level  int    `json:"level,omitempty"`
	Leader bool   `json:"leader,omitempty"`
	Battle int    `json:"battle,omitempty"`
	Help   bool   `json:"help,omitempty"`
	Action int    `json:"action,omitempty"`
	Family string `json:"family,omitempty"`
	Trade  bool   `json:"trade,omitempty"`
	Vendor string `json:"vendor,omitempty"`
	Mind   int    `json:"mind,omitempty"`
}

type NpcPos struct {
	Name string `json:"name,omitempty"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
}

type VendorEntry struct {
	Index  int    `json:"index,omitempty"`
	Kind   int    `json:"kind,omitempty"`
	Slot   int    `json:"slot,omitempty"`
	Price  int    `json:"price,omitempty"`
	Name   string `json:"name,omitempty"`
	Pile   int    `json:"pile,omitempty"`
	Image  int    `json:"image,omitempty"`
	Level  int    `json:"level,omitempty"`
	Sprite int    `json:"sprite,omitempty"`
}

type BattleFighter struct {
	Slot       int    `json:"slot,omitempty"`
	Name       string `json:"name,omitempty"`
	Sprite     int    `json:"sprite,omitempty"`
	Level      int    `json:"level,omitempty"`
	HP         int    `json:"hp,omitempty"`
	MaxHp      int    `json:"maxHp,omitempty"`
	Mp         int    `json:"mp,omitempty"`
	MaxMp      int    `json:"maxMp,omitempty"`
	RideName   string `json:"rideName,omitempty"`
	RideLevel  int    `json:"rideLevel,omitempty"`
	RideHp     int    `json:"rideHp,omitempty"`
	RideMaxHp  int    `json:"rideMaxHp,omitempty"`
	FallSprite int    `json:"fallSprite,omitempty"`
}

type InvItem struct {
	Slot        int    `json:"slot,omitempty"`
	ID          int    `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Image       int    `json:"image,omitempty"`
	Memo        string `json:"memo,omitempty"`
	Level       int    `json:"level,omitempty"`
	Pile        int    `json:"pile,omitempty"`
	Type        int    `json:"type,omitempty"`
	Field       int    `json:"field,omitempty"`
	Target      int    `json:"target,omitempty"`
	Usable      bool   `json:"usable,omitempty"`
	Equip       int    `json:"equip,omitempty"`
	Params      string `json:"params,omitempty"`
	Damage      string `json:"damage,omitempty"`
	Magic       string `json:"magic,omitempty"`
	MagicMemo   string `json:"magicMemo,omitempty"`
	MagicMp     int    `json:"magicMp,omitempty"`
	MagicField  int    `json:"magicField,omitempty"`
	MagicTarget int    `json:"magicTarget,omitempty"`
	MagicDead   bool   `json:"magicDead,omitempty"`
	MagicFunc   string `json:"magicFunc,omitempty"`
	MagicOption string `json:"magicOption,omitempty"`
	Flags       int    `json:"flags,omitempty"`
	Alch        string `json:"alch,omitempty"`
	Merged      bool   `json:"merged,omitempty"`
	Jigsaw      string `json:"jigsaw,omitempty"`
}

type BattleEvent struct {
	Actor     int    `json:"actor,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Target    int    `json:"target,omitempty"`
	Result    string `json:"result,omitempty"`
	Damage    int    `json:"damage,omitempty"`
	HP        int    `json:"hp,omitempty"`
	Dead      bool   `json:"dead,omitempty"`
	OK        bool   `json:"ok,omitempty"`
	Skill     string `json:"skill,omitempty"`
	Status    int    `json:"status,omitempty"`
	Guardian  int    `json:"guardian,omitempty"`
	Combo     bool   `json:"combo,omitempty"`
	ActorHp   int    `json:"actorHp,omitempty"`
	MpDamage  int    `json:"mpDamage,omitempty"`
	Guarded   bool   `json:"guarded,omitempty"`
	PetDamage int    `json:"petDamage,omitempty"`
	RideHp    int    `json:"rideHp,omitempty"`
	Fall      bool   `json:"fall,omitempty"`
	Effect    int    `json:"effect,omitempty"`
	ActorMp   int    `json:"actorMp,omitempty"`
	Shoot     int    `json:"shoot,omitempty"`
	Fly       int    `json:"fly,omitempty"`
	Name      string `json:"name,omitempty"`
	Sprite    int    `json:"sprite,omitempty"`
	Level     int    `json:"level,omitempty"`
	MaxHp     int    `json:"maxHp,omitempty"`
	Att       string `json:"att,omitempty"`
}

type ShopItem struct {
	Name   string `json:"name,omitempty"`
	Level  int    `json:"level,omitempty"`
	Price  int    `json:"price,omitempty"`
	Image  int    `json:"image,omitempty"`
	Info   string `json:"info,omitempty"`
	Sealed bool   `json:"sealed,omitempty"`
	Slot   int    `json:"slot,omitempty"`
	Pile   int    `json:"pile,omitempty"`
	Fame   int    `json:"fame,omitempty"`
}

type PetSkill struct {
	Slot   int    `json:"slot,omitempty"`
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Memo   string `json:"memo,omitempty"`
	Field  int    `json:"field,omitempty"`
	Target int    `json:"target,omitempty"`
	Func   string `json:"func,omitempty"`
	Option string `json:"option,omitempty"`
}

type PetInfo struct {
	Slot      int        `json:"slot,omitempty"`
	Name      string     `json:"name,omitempty"`
	FreeName  string     `json:"freeName,omitempty"`
	Sprite    int        `json:"sprite,omitempty"`
	Level     int        `json:"level,omitempty"`
	Exp       int        `json:"exp,omitempty"`
	NextExp   int        `json:"nextExp,omitempty"`
	HP        int        `json:"hp,omitempty"`
	MaxHp     int        `json:"maxHp,omitempty"`
	Atk       int        `json:"atk,omitempty"`
	Def       int        `json:"def,omitempty"`
	Agi       int        `json:"agi,omitempty"`
	Loyalty   int        `json:"loyalty,omitempty"`
	Attrs     []int      `json:"attrs,omitempty"`
	Trans     int        `json:"trans,omitempty"`
	MaxSkill  int        `json:"maxSkill,omitempty"`
	Skills    []PetSkill `json:"skills,omitempty"`
	State     string     `json:"state,omitempty"`
	CanRename bool       `json:"canRename,omitempty"`
	Rideable  bool       `json:"rideable,omitempty"`
	Items     []InvItem  `json:"items,omitempty"`
	Guard     bool       `json:"guard,omitempty"`
	Ext       PetExt     `json:"ext"`
	RideInfo  string     `json:"rideInfo,omitempty"`
}

type PetExt struct {
	Rank     int   `json:"rank,omitempty"`
	GetLevel int   `json:"getLevel,omitempty"`
	EnemyID  int   `json:"enemyId,omitempty"`
	Alloc    []int `json:"alloc,omitempty"`
	Std      []int `json:"std,omitempty"`
	Grow     []int `json:"grow,omitempty"`
	Real     []int `json:"real,omitempty"`
	Init     []int `json:"init,omitempty"`
	Resist   []int `json:"resist,omitempty"`
	Critical int   `json:"critical,omitempty"`
	Counter  int   `json:"counter,omitempty"`
	Rare     int   `json:"rare,omitempty"`
	Vai      int   `json:"vai,omitempty"`
	Magic    []int `json:"magic,omitempty"`
}

type GmItem struct {
	Cmd      string   `json:"cmd,omitempty"`
	Name     string   `json:"name,omitempty"`
	Group    string   `json:"group,omitempty"`
	Args     []string `json:"args,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Defaults []string `json:"defaults,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

type GmEntry struct {
	ID    int    `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Info  string `json:"info,omitempty"`
	Image int    `json:"image,omitempty"`
}

type QuestEntry struct {
	ID    int    `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

type AddressEntry struct {
	Slot   int    `json:"slot,omitempty"`
	Used   bool   `json:"used,omitempty"`
	Name   string `json:"name,omitempty"`
	Level  int    `json:"level,omitempty"`
	DP     int    `json:"dp,omitempty"`
	Online int    `json:"online,omitempty"`
	Face   int    `json:"face,omitempty"`
	Trans  int    `json:"trans,omitempty"`
}

type PetResult struct {
	Slot    int    `json:"slot,omitempty"`
	Name    string `json:"name,omitempty"`
	Exp     int    `json:"exp,omitempty"`
	LevelUp int    `json:"levelUp,omitempty"`
	Level   int    `json:"level,omitempty"`
}

type SetFlags struct {
	Flags int `json:"flags,omitempty"`
}

func (*SetFlags) MsgType() string { return "SetFlags" }

type CardExchange struct {
}

func (*CardExchange) MsgType() string { return "CardExchange" }

type CardDelete struct {
	Index int `json:"index,omitempty"`
}

func (*CardDelete) MsgType() string { return "CardDelete" }

type MailSend struct {
	Index int    `json:"index,omitempty"`
	Text  string `json:"text,omitempty"`
}

func (*MailSend) MsgType() string { return "MailSend" }

type PetMail struct {
	Index int    `json:"index,omitempty"`
	Pet   int    `json:"pet,omitempty"`
	Item  int    `json:"item,omitempty"`
	Text  string `json:"text,omitempty"`
}

func (*PetMail) MsgType() string { return "PetMail" }

type Family struct {
	Text string `json:"text,omitempty"`
}

func (*Family) MsgType() string { return "Family" }

type Action struct {
	X      int `json:"x,omitempty"`
	Y      int `json:"y,omitempty"`
	Action int `json:"action,omitempty"`
}

func (*Action) MsgType() string { return "Action" }

type Mind struct {
	Icon int `json:"icon,omitempty"`
}

func (*Mind) MsgType() string { return "Mind" }

type Trade struct {
	Cmd    string `json:"cmd,omitempty"`
	Slot   int    `json:"slot,omitempty"`
	Amount int    `json:"amount,omitempty"`
}

func (*Trade) MsgType() string { return "Trade" }

type Party struct {
	Request int `json:"request,omitempty"`
}

func (*Party) MsgType() string { return "Party" }

type PartyKick struct {
	Slot int `json:"slot,omitempty"`
}

func (*PartyKick) MsgType() string { return "PartyKick" }

type Vendor struct {
	Cmd    string `json:"cmd,omitempty"`
	Title  string `json:"title,omitempty"`
	Kinds  []int  `json:"kinds,omitempty"`
	Slots  []int  `json:"slots,omitempty"`
	Prices []int  `json:"prices,omitempty"`
}

func (*Vendor) MsgType() string { return "Vendor" }

type BattleHelp struct {
	On bool `json:"on,omitempty"`
}

func (*BattleHelp) MsgType() string { return "BattleHelp" }

type JoinBattle struct {
}

func (*JoinBattle) MsgType() string { return "JoinBattle" }

type Duel struct {
}

func (*Duel) MsgType() string { return "Duel" }

type Register struct {
	Account  string `json:"account,omitempty"`
	Password string `json:"password,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

func (*Register) MsgType() string { return "Register" }

type ResetPassword struct {
	Account  string `json:"account,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Password string `json:"password,omitempty"`
}

func (*ResetPassword) MsgType() string { return "ResetPassword" }

type AccountInfoReq struct {
}

func (*AccountInfoReq) MsgType() string { return "AccountInfoReq" }

type ChangePassword struct {
	Old      string `json:"old,omitempty"`
	Password string `json:"password,omitempty"`
}

func (*ChangePassword) MsgType() string { return "ChangePassword" }

type ChangeSecret struct {
	Password string `json:"password,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

func (*ChangeSecret) MsgType() string { return "ChangeSecret" }

type Login struct {
	Account  string `json:"account,omitempty"`
	Password string `json:"password,omitempty"`
}

func (*Login) MsgType() string { return "Login" }

type SelectLine struct {
	ID int `json:"id,omitempty"`
}

func (*SelectLine) MsgType() string { return "SelectLine" }

type CharList struct {
}

func (*CharList) MsgType() string { return "CharList" }

type CharCreate struct {
	Name     string `json:"name,omitempty"`
	Sprite   int    `json:"sprite,omitempty"`
	Face     int    `json:"face,omitempty"`
	Vital    int    `json:"vital,omitempty"`
	Str      int    `json:"str,omitempty"`
	Tgh      int    `json:"tgh,omitempty"`
	Dex      int    `json:"dex,omitempty"`
	Earth    int    `json:"earth,omitempty"`
	Water    int    `json:"water,omitempty"`
	Fire     int    `json:"fire,omitempty"`
	Wind     int    `json:"wind,omitempty"`
	Hometown int    `json:"hometown,omitempty"`
}

func (*CharCreate) MsgType() string { return "CharCreate" }

type CharDelete struct {
	ID int `json:"id,omitempty"`
}

func (*CharDelete) MsgType() string { return "CharDelete" }

type CharEnter struct {
	ID int `json:"id,omitempty"`
}

func (*CharEnter) MsgType() string { return "CharEnter" }

type MapReq struct {
	Floor int `json:"floor,omitempty"`
	X1    int `json:"x1,omitempty"`
	Y1    int `json:"y1,omitempty"`
	X2    int `json:"x2,omitempty"`
	Y2    int `json:"y2,omitempty"`
}

func (*MapReq) MsgType() string { return "MapReq" }

type NpcListReq struct {
	Floor int `json:"floor,omitempty"`
}

func (*NpcListReq) MsgType() string { return "NpcListReq" }

type Move struct {
	X   int `json:"x,omitempty"`
	Y   int `json:"y,omitempty"`
	Dir int `json:"dir,omitempty"`
}

func (*Move) MsgType() string { return "Move" }

type Turn struct {
	Dir int `json:"dir,omitempty"`
}

func (*Turn) MsgType() string { return "Turn" }

type Talk struct {
	ID int `json:"id,omitempty"`
}

func (*Talk) MsgType() string { return "Talk" }

type Chat struct {
	Channel string `json:"channel,omitempty"`
	Text    string `json:"text,omitempty"`
	Color   int    `json:"color,omitempty"`
	Area    int    `json:"area,omitempty"`
}

func (*Chat) MsgType() string { return "Chat" }

type ReturnHome struct {
}

func (*ReturnHome) MsgType() string { return "ReturnHome" }

type BattleCommand struct {
	Kind   string `json:"kind,omitempty"`
	Target int    `json:"target,omitempty"`
	Item   int    `json:"item,omitempty"`
}

func (*BattleCommand) MsgType() string { return "BattleCommand" }

type BattlePetCommand struct {
	Skill  int `json:"skill,omitempty"`
	Target int `json:"target,omitempty"`
}

func (*BattlePetCommand) MsgType() string { return "BattlePetCommand" }

type PetState struct {
	Slot  int    `json:"slot,omitempty"`
	State string `json:"state,omitempty"`
}

func (*PetState) MsgType() string { return "PetState" }

type PetRename struct {
	Slot int    `json:"slot,omitempty"`
	Name string `json:"name,omitempty"`
}

func (*PetRename) MsgType() string { return "PetRename" }

type PetDrop struct {
	Slot int `json:"slot,omitempty"`
}

func (*PetDrop) MsgType() string { return "PetDrop" }

type PetRide struct {
	Slot int `json:"slot,omitempty"`
}

func (*PetRide) MsgType() string { return "PetRide" }

type ItemMove struct {
	From int `json:"from,omitempty"`
	To   int `json:"to,omitempty"`
}

func (*ItemMove) MsgType() string { return "ItemMove" }

type PetItemEquip struct {
	Pet  int `json:"pet,omitempty"`
	From int `json:"from,omitempty"`
	To   int `json:"to,omitempty"`
}

func (*PetItemEquip) MsgType() string { return "PetItemEquip" }

type PetSkillUse struct {
	Pet   int    `json:"pet,omitempty"`
	Skill int    `json:"skill,omitempty"`
	Data  string `json:"data,omitempty"`
}

func (*PetSkillUse) MsgType() string { return "PetSkillUse" }

type ItemUse struct {
	Slot   int `json:"slot,omitempty"`
	Target int `json:"target,omitempty"`
}

func (*ItemUse) MsgType() string { return "ItemUse" }

type MagicUse struct {
	Slot   int `json:"slot,omitempty"`
	Target int `json:"target,omitempty"`
}

func (*MagicUse) MsgType() string { return "MagicUse" }

type ItemDrop struct {
	Slot int `json:"slot,omitempty"`
}

func (*ItemDrop) MsgType() string { return "ItemDrop" }

type GoldDrop struct {
	Amount int `json:"amount,omitempty"`
}

func (*GoldDrop) MsgType() string { return "GoldDrop" }

type PickUp struct {
	Dir int `json:"dir,omitempty"`
}

func (*PickUp) MsgType() string { return "PickUp" }

type StatUp struct {
	Stat int `json:"stat,omitempty"`
}

func (*StatUp) MsgType() string { return "StatUp" }

type NpcWindowReply struct {
	ID     int    `json:"id,omitempty"`
	Seq    int    `json:"seq,omitempty"`
	Button int    `json:"button,omitempty"`
	Data   string `json:"data,omitempty"`
}

func (*NpcWindowReply) MsgType() string { return "NpcWindowReply" }

type Ping struct {
	Seq int `json:"seq,omitempty"`
}

func (*Ping) MsgType() string { return "Ping" }

type GmCommand struct {
	Text string `json:"text,omitempty"`
}

func (*GmCommand) MsgType() string { return "GmCommand" }

type GmListReq struct {
	Kind string `json:"kind,omitempty"`
}

func (*GmListReq) MsgType() string { return "GmListReq" }

type QuestReq struct {
}

func (*QuestReq) MsgType() string { return "QuestReq" }

type GmDetailReq struct {
	Kind string `json:"kind,omitempty"`
	ID   int    `json:"id,omitempty"`
}

func (*GmDetailReq) MsgType() string { return "GmDetailReq" }

type Logout struct {
}

func (*Logout) MsgType() string { return "Logout" }

type Flags struct {
	Flags int `json:"flags,omitempty"`
}

func (*Flags) MsgType() string { return "Flags" }

type GmMenu struct {
	Items []GmItem `json:"items,omitempty"`
}

func (*GmMenu) MsgType() string { return "GmMenu" }

type GmList struct {
	Kind    string    `json:"kind,omitempty"`
	Entries []GmEntry `json:"entries,omitempty"`
}

func (*GmList) MsgType() string { return "GmList" }

type QuestLog struct {
	Quests    []QuestEntry `json:"quests,omitempty"`
	Available []QuestEntry `json:"available,omitempty"`
}

func (*QuestLog) MsgType() string { return "QuestLog" }

type GmDetail struct {
	Kind  string   `json:"kind,omitempty"`
	ID    int      `json:"id,omitempty"`
	Image int      `json:"image,omitempty"`
	Lines []string `json:"lines,omitempty"`
}

func (*GmDetail) MsgType() string { return "GmDetail" }

type DuelResult struct {
	Delta int `json:"delta,omitempty"`
	DP    int `json:"dp,omitempty"`
}

func (*DuelResult) MsgType() string { return "DuelResult" }

type FamilyMsg struct {
	Text string `json:"text,omitempty"`
}

func (*FamilyMsg) MsgType() string { return "FamilyMsg" }

type FamilyStatus struct {
	Index   int    `json:"index,omitempty"`
	Name    string `json:"name,omitempty"`
	Duty    int    `json:"duty,omitempty"`
	Channel int    `json:"channel,omitempty"`
	Sprite  int    `json:"sprite,omitempty"`
}

func (*FamilyStatus) MsgType() string { return "FamilyStatus" }

type ObjFamily struct {
	ID     int    `json:"id,omitempty"`
	Family string `json:"family,omitempty"`
}

func (*ObjFamily) MsgType() string { return "ObjFamily" }

type AddressBook struct {
	Entries []AddressEntry `json:"entries,omitempty"`
}

func (*AddressBook) MsgType() string { return "AddressBook" }

type AddressUpdate struct {
	Entries []AddressEntry `json:"entries,omitempty"`
}

func (*AddressUpdate) MsgType() string { return "AddressUpdate" }

type MailRecv struct {
	Index     int    `json:"index,omitempty"`
	Date      string `json:"date,omitempty"`
	Text      string `json:"text,omitempty"`
	PetSprite int    `json:"petSprite,omitempty"`
	PetLevel  int    `json:"petLevel,omitempty"`
	PetName   string `json:"petName,omitempty"`
	ItemImage int    `json:"itemImage,omitempty"`
}

func (*MailRecv) MsgType() string { return "MailRecv" }

type ObjAction struct {
	ID     int `json:"id,omitempty"`
	Action int `json:"action,omitempty"`
}

func (*ObjAction) MsgType() string { return "ObjAction" }

type ObjMind struct {
	ID   int `json:"id,omitempty"`
	Icon int `json:"icon,omitempty"`
}

func (*ObjMind) MsgType() string { return "ObjMind" }

type TradeOpen struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (*TradeOpen) MsgType() string { return "TradeOpen" }

type TradeItem struct {
	Slot  int       `json:"slot,omitempty"`
	Count int       `json:"count,omitempty"`
	Items []InvItem `json:"items,omitempty"`
}

func (*TradeItem) MsgType() string { return "TradeItem" }

type TradeGold struct {
	Amount int `json:"amount,omitempty"`
}

func (*TradeGold) MsgType() string { return "TradeGold" }

type TradePet struct {
	Pets []PetInfo `json:"pets,omitempty"`
}

func (*TradePet) MsgType() string { return "TradePet" }

type TradeState struct {
	State string `json:"state,omitempty"`
}

func (*TradeState) MsgType() string { return "TradeState" }

type TradeClose struct {
}

func (*TradeClose) MsgType() string { return "TradeClose" }

type PartyResult struct {
	Request int  `json:"request,omitempty"`
	OK      bool `json:"ok,omitempty"`
}

func (*PartyResult) MsgType() string { return "PartyResult" }

type PartyMember struct {
	Slot  int    `json:"slot,omitempty"`
	Used  bool   `json:"used,omitempty"`
	ID    int    `json:"id,omitempty"`
	Level int    `json:"level,omitempty"`
	MaxHp int    `json:"maxHp,omitempty"`
	HP    int    `json:"hp,omitempty"`
	Mp    int    `json:"mp,omitempty"`
	Name  string `json:"name,omitempty"`
}

func (*PartyMember) MsgType() string { return "PartyMember" }

type ObjMark struct {
	ID     int    `json:"id,omitempty"`
	Leader bool   `json:"leader,omitempty"`
	Battle int    `json:"battle,omitempty"`
	Help   bool   `json:"help,omitempty"`
	Trade  bool   `json:"trade,omitempty"`
	Vendor string `json:"vendor,omitempty"`
}

func (*ObjMark) MsgType() string { return "ObjMark" }

type VendorSetup struct {
	Entries []VendorEntry `json:"entries,omitempty"`
}

func (*VendorSetup) MsgType() string { return "VendorSetup" }

type VendorList struct {
	Seller  string        `json:"seller,omitempty"`
	Title   string        `json:"title,omitempty"`
	Entries []VendorEntry `json:"entries,omitempty"`
}

func (*VendorList) MsgType() string { return "VendorList" }

type VendorClose struct {
}

func (*VendorClose) MsgType() string { return "VendorClose" }

type VendorDetail struct {
	Index int     `json:"index,omitempty"`
	Kind  int     `json:"kind,omitempty"`
	Item  InvItem `json:"item"`
	Pet   PetInfo `json:"pet"`
}

func (*VendorDetail) MsgType() string { return "VendorDetail" }

type Sound struct {
	X  int `json:"x,omitempty"`
	Y  int `json:"y,omitempty"`
	Se int `json:"se,omitempty"`
}

func (*Sound) MsgType() string { return "Sound" }

type HelpFlag struct {
	On bool `json:"on,omitempty"`
}

func (*HelpFlag) MsgType() string { return "HelpFlag" }

type BattleJoin struct {
	Fighters []BattleFighter `json:"fighters,omitempty"`
}

func (*BattleJoin) MsgType() string { return "BattleJoin" }

type Hello struct {
	Version    string `json:"version,omitempty"`
	ServerTime int    `json:"serverTime,omitempty"`
}

func (*Hello) MsgType() string { return "Hello" }

type PetSkillResult struct {
	OK    bool `json:"ok,omitempty"`
	Pet   int  `json:"pet,omitempty"`
	Skill int  `json:"skill,omitempty"`
}

func (*PetSkillResult) MsgType() string { return "PetSkillResult" }

type Error struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (*Error) MsgType() string { return "Error" }

type AuthResult struct {
	OK      bool   `json:"ok,omitempty"`
	Message string `json:"message,omitempty"`
}

func (*AuthResult) MsgType() string { return "AuthResult" }

type AccountInfo struct {
	Account   string `json:"account,omitempty"`
	Created   string `json:"created,omitempty"`
	Chars     int    `json:"chars,omitempty"`
	HasSecret bool   `json:"hasSecret,omitempty"`
}

func (*AccountInfo) MsgType() string { return "AccountInfo" }

type AccountResult struct {
	OK      bool   `json:"ok,omitempty"`
	Message string `json:"message,omitempty"`
}

func (*AccountResult) MsgType() string { return "AccountResult" }

type ServerList struct {
	Groups []ServerGroup `json:"groups,omitempty"`
}

func (*ServerList) MsgType() string { return "ServerList" }

type LineSelected struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (*LineSelected) MsgType() string { return "LineSelected" }

type CharListResult struct {
	Chars    []CharSummary `json:"chars,omitempty"`
	MaxChars int           `json:"maxChars,omitempty"`
}

func (*CharListResult) MsgType() string { return "CharListResult" }

type CharCreateResult struct {
	OK      bool   `json:"ok,omitempty"`
	Message string `json:"message,omitempty"`
	ID      int    `json:"id,omitempty"`
}

func (*CharCreateResult) MsgType() string { return "CharCreateResult" }

type Self struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Sprite int    `json:"sprite,omitempty"`
}

func (*Self) MsgType() string { return "Self" }

type Status struct {
	Name     string   `json:"name,omitempty"`
	Face     int      `json:"face,omitempty"`
	Level    int      `json:"level,omitempty"`
	Exp      int      `json:"exp,omitempty"`
	NextExp  int      `json:"nextExp,omitempty"`
	HP       int      `json:"hp,omitempty"`
	MaxHp    int      `json:"maxHp,omitempty"`
	Mp       int      `json:"mp,omitempty"`
	MaxMp    int      `json:"maxMp,omitempty"`
	Atk      int      `json:"atk,omitempty"`
	Def      int      `json:"def,omitempty"`
	Agi      int      `json:"agi,omitempty"`
	Charm    int      `json:"charm,omitempty"`
	DP       int      `json:"dp,omitempty"`
	Vital    int      `json:"vital,omitempty"`
	Str      int      `json:"str,omitempty"`
	Tgh      int      `json:"tgh,omitempty"`
	Dex      int      `json:"dex,omitempty"`
	Attrs    []int    `json:"attrs,omitempty"`
	Hometown int      `json:"hometown,omitempty"`
	Trans    int      `json:"trans,omitempty"`
	Points   int      `json:"points,omitempty"`
	Detail   []string `json:"detail,omitempty"`
}

func (*Status) MsgType() string { return "Status" }

type EnterFloor struct {
	Floor   int    `json:"floor,omitempty"`
	Name    string `json:"name,omitempty"`
	Palette int    `json:"palette,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	X       int    `json:"x,omitempty"`
	Y       int    `json:"y,omitempty"`
	Dir     int    `json:"dir,omitempty"`
	Step    int    `json:"step,omitempty"`
	MapVer  string `json:"mapVer,omitempty"`
}

func (*EnterFloor) MsgType() string { return "EnterFloor" }

type MapData struct {
	Floor int    `json:"floor,omitempty"`
	X1    int    `json:"x1,omitempty"`
	Y1    int    `json:"y1,omitempty"`
	X2    int    `json:"x2,omitempty"`
	Y2    int    `json:"y2,omitempty"`
	Tiles []int  `json:"tiles,omitempty"`
	Objs  []int  `json:"objs,omitempty"`
	Walk  string `json:"walk,omitempty"`
	Ver   string `json:"ver,omitempty"`
}

func (*MapData) MsgType() string { return "MapData" }

type NpcList struct {
	Floor int      `json:"floor,omitempty"`
	Npcs  []NpcPos `json:"npcs,omitempty"`
}

func (*NpcList) MsgType() string { return "NpcList" }

type PosCorrect struct {
	X   int `json:"x,omitempty"`
	Y   int `json:"y,omitempty"`
	Dir int `json:"dir,omitempty"`
}

func (*PosCorrect) MsgType() string { return "PosCorrect" }

type ObjAppear struct {
	Objs []ObjInfo `json:"objs,omitempty"`
}

func (*ObjAppear) MsgType() string { return "ObjAppear" }

type ObjDisappear struct {
	IDs []int `json:"ids,omitempty"`
}

func (*ObjDisappear) MsgType() string { return "ObjDisappear" }

type ObjMove struct {
	ID  int `json:"id,omitempty"`
	X   int `json:"x,omitempty"`
	Y   int `json:"y,omitempty"`
	Dir int `json:"dir,omitempty"`
}

func (*ObjMove) MsgType() string { return "ObjMove" }

type ObjTurn struct {
	ID  int `json:"id,omitempty"`
	Dir int `json:"dir,omitempty"`
}

func (*ObjTurn) MsgType() string { return "ObjTurn" }

type ObjSprite struct {
	ID     int `json:"id,omitempty"`
	Sprite int `json:"sprite,omitempty"`
}

func (*ObjSprite) MsgType() string { return "ObjSprite" }

type NpcTalk struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Text   string `json:"text,omitempty"`
	Window bool   `json:"window,omitempty"`
}

func (*NpcTalk) MsgType() string { return "NpcTalk" }

type NpcWindow struct {
	ID      int    `json:"id,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Buttons int    `json:"buttons,omitempty"`
	Seq     int    `json:"seq,omitempty"`
	Text    string `json:"text,omitempty"`
}

func (*NpcWindow) MsgType() string { return "NpcWindow" }

type ShopWindow struct {
	ID       int        `json:"id,omitempty"`
	Seq      int        `json:"seq,omitempty"`
	Mode     string     `json:"mode,omitempty"`
	Title    string     `json:"title,omitempty"`
	Msg      string     `json:"msg,omitempty"`
	Main     string     `json:"main,omitempty"`
	What     string     `json:"what,omitempty"`
	Level    string     `json:"level,omitempty"`
	Realy    string     `json:"realy,omitempty"`
	Full     string     `json:"full,omitempty"`
	Stone    string     `json:"stone,omitempty"`
	SellOnly bool       `json:"sellOnly,omitempty"`
	MaxGold  int        `json:"maxGold,omitempty"`
	Fame     int        `json:"fame,omitempty"`
	Rest     int        `json:"rest,omitempty"`
	Items    []ShopItem `json:"items,omitempty"`
}

func (*ShopWindow) MsgType() string { return "ShopWindow" }

type ChatMsg struct {
	Channel string `json:"channel,omitempty"`
	FromID  int    `json:"fromId,omitempty"`
	From    string `json:"from,omitempty"`
	Text    string `json:"text,omitempty"`
	Color   int    `json:"color,omitempty"`
}

func (*ChatMsg) MsgType() string { return "ChatMsg" }

type Pong struct {
	Seq        int `json:"seq,omitempty"`
	ServerTime int `json:"serverTime,omitempty"`
}

func (*Pong) MsgType() string { return "Pong" }

type BattleStart struct {
	Field    int             `json:"field,omitempty"`
	MySlot   int             `json:"mySlot,omitempty"`
	TurnTime int             `json:"turnTime,omitempty"`
	Surprise int             `json:"surprise,omitempty"`
	Fighters []BattleFighter `json:"fighters,omitempty"`
	Pvp      bool            `json:"pvp,omitempty"`
	Event    bool            `json:"event,omitempty"`
	Watch    bool            `json:"watch,omitempty"`
	Speed    int             `json:"speed,omitempty"`
}

func (*BattleStart) MsgType() string { return "BattleStart" }

type BattleTurn struct {
	Turn   int           `json:"turn,omitempty"`
	Events []BattleEvent `json:"events,omitempty"`
}

func (*BattleTurn) MsgType() string { return "BattleTurn" }

type BattleEnd struct {
	Result  string      `json:"result,omitempty"`
	Exp     int         `json:"exp,omitempty"`
	LevelUp int         `json:"levelUp,omitempty"`
	Level   int         `json:"level,omitempty"`
	Items   []string    `json:"items,omitempty"`
	Pets    []PetResult `json:"pets,omitempty"`
}

func (*BattleEnd) MsgType() string { return "BattleEnd" }

type Pets struct {
	Battle int       `json:"battle,omitempty"`
	Ride   int       `json:"ride,omitempty"`
	Pets   []PetInfo `json:"pets,omitempty"`
}

func (*Pets) MsgType() string { return "Pets" }

type Inventory struct {
	Gold  int       `json:"gold,omitempty"`
	Items []InvItem `json:"items,omitempty"`
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
	case "Mind":
		m = &Mind{}
	case "Trade":
		m = &Trade{}
	case "Party":
		m = &Party{}
	case "PartyKick":
		m = &PartyKick{}
	case "Vendor":
		m = &Vendor{}
	case "BattleHelp":
		m = &BattleHelp{}
	case "JoinBattle":
		m = &JoinBattle{}
	case "Duel":
		m = &Duel{}
	case "Register":
		m = &Register{}
	case "ResetPassword":
		m = &ResetPassword{}
	case "AccountInfoReq":
		m = &AccountInfoReq{}
	case "ChangePassword":
		m = &ChangePassword{}
	case "ChangeSecret":
		m = &ChangeSecret{}
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
	case "NpcListReq":
		m = &NpcListReq{}
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
	case "QuestReq":
		m = &QuestReq{}
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
