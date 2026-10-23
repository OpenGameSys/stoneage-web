// 由 server/tools/protogen 根据 proto/messages.json 生成，请勿手改。

export interface CharSummary { id: number; name: string; sprite: number; face: number; level: number; hp: number; atk: number; def: number; agi: number; charm: number; dp: number; attrs: number[]; logins: number; hometown: number; }
export interface LineInfo { id: number; name: string; online: number; }
export interface ServerGroup { name: string; lines: LineInfo[]; }
export interface ObjInfo { id: number; kind: string; name: string; sprite: number; x: number; y: number; dir: number; level: number; leader: boolean; battle: number; help: boolean; action: number; family: string; trade: boolean; vendor: string; }
export interface NpcPos { name: string; x: number; y: number; }
export interface VendorEntry { index: number; kind: number; slot: number; price: number; name: string; pile: number; image: number; level: number; sprite: number; }
export interface BattleFighter { slot: number; name: string; sprite: number; level: number; hp: number; maxHp: number; mp: number; maxMp: number; rideName: string; rideLevel: number; rideHp: number; rideMaxHp: number; fallSprite: number; }
export interface InvItem { slot: number; id: number; name: string; image: number; memo: string; level: number; pile: number; type: number; field: number; target: number; usable: boolean; equip: number; params: string; damage: string; magic: string; magicMemo: string; magicMp: number; magicField: number; magicTarget: number; magicDead: boolean; magicFunc: string; magicOption: string; flags: number; alch: string; merged: boolean; jigsaw: string; }
export interface BattleEvent { actor: number; kind: string; target: number; result: string; damage: number; hp: number; dead: boolean; ok: boolean; skill: string; status: number; guardian: number; combo: boolean; actorHp: number; mpDamage: number; guarded: boolean; petDamage: number; rideHp: number; fall: boolean; effect: number; actorMp: number; name: string; sprite: number; level: number; maxHp: number; att: string; }
export interface ShopItem { name: string; level: number; price: number; image: number; info: string; sealed: boolean; slot: number; pile: number; fame: number; }
export interface PetSkill { slot: number; id: number; name: string; memo: string; field: number; target: number; func: string; option: string; }
export interface PetInfo { slot: number; name: string; freeName: string; sprite: number; level: number; exp: number; nextExp: number; hp: number; maxHp: number; atk: number; def: number; agi: number; loyalty: number; attrs: number[]; trans: number; maxSkill: number; skills: PetSkill[]; state: string; canRename: boolean; rideable: boolean; items: InvItem[]; guard: boolean; ext: PetExt; rideInfo: string; }
export interface PetExt { rank: number; getLevel: number; enemyId: number; alloc: number[]; std: number[]; grow: number[]; real: number[]; init: number[]; resist: number[]; critical: number; counter: number; rare: number; vai: number; magic: number[]; }
export interface GmItem { cmd: string; name: string; group: string; args: string[]; kinds: string[]; defaults: string[]; hint: string; }
export interface GmEntry { id: number; name: string; info: string; image: number; }
export interface QuestEntry { id: number; name: string; state: string; hint: string; }
export interface AddressEntry { slot: number; used: boolean; name: string; level: number; dp: number; online: number; face: number; trans: number; }
export interface PetResult { slot: number; name: string; exp: number; levelUp: number; level: number; }

export interface C2SSetFlags { t: "SetFlags"; flags: number; }
export interface C2SCardExchange { t: "CardExchange"; }
export interface C2SCardDelete { t: "CardDelete"; index: number; }
export interface C2SMailSend { t: "MailSend"; index: number; text: string; }
export interface C2SPetMail { t: "PetMail"; index: number; pet: number; item: number; text: string; }
export interface C2SFamily { t: "Family"; text: string; }
export interface C2SAction { t: "Action"; x: number; y: number; action: number; }
export interface C2STrade { t: "Trade"; cmd: string; slot: number; amount: number; }
export interface C2SParty { t: "Party"; request: number; }
export interface C2SPartyKick { t: "PartyKick"; slot: number; }
export interface C2SVendor { t: "Vendor"; cmd: string; title: string; kinds: number[]; slots: number[]; prices: number[]; }
export interface C2SBattleHelp { t: "BattleHelp"; on: boolean; }
export interface C2SJoinBattle { t: "JoinBattle"; }
export interface C2SDuel { t: "Duel"; }
export interface C2SRegister { t: "Register"; account: string; password: string; secret: string; }
export interface C2SResetPassword { t: "ResetPassword"; account: string; secret: string; password: string; }
export interface C2SAccountInfoReq { t: "AccountInfoReq"; }
export interface C2SChangePassword { t: "ChangePassword"; old: string; password: string; }
export interface C2SChangeSecret { t: "ChangeSecret"; password: string; secret: string; }
export interface C2SLogin { t: "Login"; account: string; password: string; }
export interface C2SSelectLine { t: "SelectLine"; id: number; }
export interface C2SCharList { t: "CharList"; }
export interface C2SCharCreate { t: "CharCreate"; name: string; sprite: number; face: number; vital: number; str: number; tgh: number; dex: number; earth: number; water: number; fire: number; wind: number; hometown: number; }
export interface C2SCharDelete { t: "CharDelete"; id: number; }
export interface C2SCharEnter { t: "CharEnter"; id: number; }
export interface C2SMapReq { t: "MapReq"; floor: number; x1: number; y1: number; x2: number; y2: number; }
export interface C2SNpcListReq { t: "NpcListReq"; floor: number; }
export interface C2SMove { t: "Move"; x: number; y: number; dir: number; }
export interface C2STurn { t: "Turn"; dir: number; }
export interface C2STalk { t: "Talk"; id: number; }
export interface C2SChat { t: "Chat"; channel: string; text: string; color: number; area: number; }
export interface C2SReturnHome { t: "ReturnHome"; }
export interface C2SBattleCommand { t: "BattleCommand"; kind: string; target: number; item: number; }
export interface C2SBattlePetCommand { t: "BattlePetCommand"; skill: number; target: number; }
export interface C2SPetState { t: "PetState"; slot: number; state: string; }
export interface C2SPetRename { t: "PetRename"; slot: number; name: string; }
export interface C2SPetDrop { t: "PetDrop"; slot: number; }
export interface C2SPetRide { t: "PetRide"; slot: number; }
export interface C2SItemMove { t: "ItemMove"; from: number; to: number; }
export interface C2SPetItemEquip { t: "PetItemEquip"; pet: number; from: number; to: number; }
export interface C2SPetSkillUse { t: "PetSkillUse"; pet: number; skill: number; data: string; }
export interface C2SItemUse { t: "ItemUse"; slot: number; target: number; }
export interface C2SMagicUse { t: "MagicUse"; slot: number; target: number; }
export interface C2SItemDrop { t: "ItemDrop"; slot: number; }
export interface C2SGoldDrop { t: "GoldDrop"; amount: number; }
export interface C2SPickUp { t: "PickUp"; dir: number; }
export interface C2SStatUp { t: "StatUp"; stat: number; }
export interface C2SNpcWindowReply { t: "NpcWindowReply"; id: number; seq: number; button: number; data: string; }
export interface C2SPing { t: "Ping"; seq: number; }
export interface C2SGmCommand { t: "GmCommand"; text: string; }
export interface C2SGmListReq { t: "GmListReq"; kind: string; }
export interface C2SQuestReq { t: "QuestReq"; }
export interface C2SGmDetailReq { t: "GmDetailReq"; kind: string; id: number; }
export interface C2SLogout { t: "Logout"; }
export type C2S = C2SSetFlags | C2SCardExchange | C2SCardDelete | C2SMailSend | C2SPetMail | C2SFamily | C2SAction | C2STrade | C2SParty | C2SPartyKick | C2SVendor | C2SBattleHelp | C2SJoinBattle | C2SDuel | C2SRegister | C2SResetPassword | C2SAccountInfoReq | C2SChangePassword | C2SChangeSecret | C2SLogin | C2SSelectLine | C2SCharList | C2SCharCreate | C2SCharDelete | C2SCharEnter | C2SMapReq | C2SNpcListReq | C2SMove | C2STurn | C2STalk | C2SChat | C2SReturnHome | C2SBattleCommand | C2SBattlePetCommand | C2SPetState | C2SPetRename | C2SPetDrop | C2SPetRide | C2SItemMove | C2SPetItemEquip | C2SPetSkillUse | C2SItemUse | C2SMagicUse | C2SItemDrop | C2SGoldDrop | C2SPickUp | C2SStatUp | C2SNpcWindowReply | C2SPing | C2SGmCommand | C2SGmListReq | C2SQuestReq | C2SGmDetailReq | C2SLogout;
export type C2SType = C2S["t"];

export interface S2CFlags { t: "Flags"; flags: number; }
export interface S2CGmMenu { t: "GmMenu"; items: GmItem[]; }
export interface S2CGmList { t: "GmList"; kind: string; entries: GmEntry[]; }
export interface S2CQuestLog { t: "QuestLog"; quests: QuestEntry[]; }
export interface S2CGmDetail { t: "GmDetail"; kind: string; id: number; image: number; lines: string[]; }
export interface S2CDuelResult { t: "DuelResult"; delta: number; dp: number; }
export interface S2CFamilyMsg { t: "FamilyMsg"; text: string; }
export interface S2CFamilyStatus { t: "FamilyStatus"; index: number; name: string; duty: number; channel: number; sprite: number; }
export interface S2CObjFamily { t: "ObjFamily"; id: number; family: string; }
export interface S2CAddressBook { t: "AddressBook"; entries: AddressEntry[]; }
export interface S2CAddressUpdate { t: "AddressUpdate"; entries: AddressEntry[]; }
export interface S2CMailRecv { t: "MailRecv"; index: number; date: string; text: string; petSprite: number; petLevel: number; petName: string; itemImage: number; }
export interface S2CObjAction { t: "ObjAction"; id: number; action: number; }
export interface S2CTradeOpen { t: "TradeOpen"; id: number; name: string; }
export interface S2CTradeItem { t: "TradeItem"; slot: number; count: number; items: InvItem[]; }
export interface S2CTradeGold { t: "TradeGold"; amount: number; }
export interface S2CTradePet { t: "TradePet"; pets: PetInfo[]; }
export interface S2CTradeState { t: "TradeState"; state: string; }
export interface S2CTradeClose { t: "TradeClose"; }
export interface S2CPartyResult { t: "PartyResult"; request: number; ok: boolean; }
export interface S2CPartyMember { t: "PartyMember"; slot: number; used: boolean; id: number; level: number; maxHp: number; hp: number; mp: number; name: string; }
export interface S2CObjMark { t: "ObjMark"; id: number; leader: boolean; battle: number; help: boolean; trade: boolean; vendor: string; }
export interface S2CVendorSetup { t: "VendorSetup"; entries: VendorEntry[]; }
export interface S2CVendorList { t: "VendorList"; seller: string; title: string; entries: VendorEntry[]; }
export interface S2CVendorClose { t: "VendorClose"; }
export interface S2CVendorDetail { t: "VendorDetail"; index: number; kind: number; item: InvItem; pet: PetInfo; }
export interface S2CSound { t: "Sound"; x: number; y: number; se: number; }
export interface S2CHelpFlag { t: "HelpFlag"; on: boolean; }
export interface S2CBattleJoin { t: "BattleJoin"; fighters: BattleFighter[]; }
export interface S2CHello { t: "Hello"; version: string; serverTime: number; }
export interface S2CPetSkillResult { t: "PetSkillResult"; ok: boolean; pet: number; skill: number; }
export interface S2CError { t: "Error"; code: string; message: string; }
export interface S2CAuthResult { t: "AuthResult"; ok: boolean; message: string; }
export interface S2CAccountInfo { t: "AccountInfo"; account: string; created: string; chars: number; hasSecret: boolean; }
export interface S2CAccountResult { t: "AccountResult"; ok: boolean; message: string; }
export interface S2CServerList { t: "ServerList"; groups: ServerGroup[]; }
export interface S2CLineSelected { t: "LineSelected"; id: number; name: string; }
export interface S2CCharListResult { t: "CharListResult"; chars: CharSummary[]; maxChars: number; }
export interface S2CCharCreateResult { t: "CharCreateResult"; ok: boolean; message: string; id: number; }
export interface S2CSelf { t: "Self"; id: number; name: string; sprite: number; }
export interface S2CStatus { t: "Status"; name: string; face: number; level: number; exp: number; nextExp: number; hp: number; maxHp: number; mp: number; maxMp: number; atk: number; def: number; agi: number; charm: number; dp: number; vital: number; str: number; tgh: number; dex: number; attrs: number[]; hometown: number; trans: number; points: number; }
export interface S2CEnterFloor { t: "EnterFloor"; floor: number; name: string; palette: number; width: number; height: number; x: number; y: number; dir: number; step: number; }
export interface S2CMapData { t: "MapData"; floor: number; x1: number; y1: number; x2: number; y2: number; tiles: number[]; objs: number[]; walk: string; }
export interface S2CNpcList { t: "NpcList"; floor: number; npcs: NpcPos[]; }
export interface S2CPosCorrect { t: "PosCorrect"; x: number; y: number; dir: number; }
export interface S2CObjAppear { t: "ObjAppear"; objs: ObjInfo[]; }
export interface S2CObjDisappear { t: "ObjDisappear"; ids: number[]; }
export interface S2CObjMove { t: "ObjMove"; id: number; x: number; y: number; dir: number; }
export interface S2CObjTurn { t: "ObjTurn"; id: number; dir: number; }
export interface S2CObjSprite { t: "ObjSprite"; id: number; sprite: number; }
export interface S2CNpcTalk { t: "NpcTalk"; id: number; name: string; text: string; window: boolean; }
export interface S2CNpcWindow { t: "NpcWindow"; id: number; kind: string; buttons: number; seq: number; text: string; }
export interface S2CShopWindow { t: "ShopWindow"; id: number; seq: number; mode: string; title: string; msg: string; main: string; what: string; level: string; realy: string; full: string; stone: string; sellOnly: boolean; maxGold: number; fame: number; rest: number; items: ShopItem[]; }
export interface S2CChatMsg { t: "ChatMsg"; channel: string; fromId: number; from: string; text: string; color: number; }
export interface S2CPong { t: "Pong"; seq: number; serverTime: number; }
export interface S2CBattleStart { t: "BattleStart"; field: number; mySlot: number; turnTime: number; surprise: number; fighters: BattleFighter[]; pvp: boolean; event: boolean; watch: boolean; }
export interface S2CBattleTurn { t: "BattleTurn"; turn: number; events: BattleEvent[]; }
export interface S2CBattleEnd { t: "BattleEnd"; result: string; exp: number; levelUp: number; level: number; items: string[]; pets: PetResult[]; }
export interface S2CPets { t: "Pets"; battle: number; ride: number; pets: PetInfo[]; }
export interface S2CInventory { t: "Inventory"; gold: number; items: InvItem[]; }
export type S2C = S2CFlags | S2CGmMenu | S2CGmList | S2CQuestLog | S2CGmDetail | S2CDuelResult | S2CFamilyMsg | S2CFamilyStatus | S2CObjFamily | S2CAddressBook | S2CAddressUpdate | S2CMailRecv | S2CObjAction | S2CTradeOpen | S2CTradeItem | S2CTradeGold | S2CTradePet | S2CTradeState | S2CTradeClose | S2CPartyResult | S2CPartyMember | S2CObjMark | S2CVendorSetup | S2CVendorList | S2CVendorClose | S2CVendorDetail | S2CSound | S2CHelpFlag | S2CBattleJoin | S2CHello | S2CPetSkillResult | S2CError | S2CAuthResult | S2CAccountInfo | S2CAccountResult | S2CServerList | S2CLineSelected | S2CCharListResult | S2CCharCreateResult | S2CSelf | S2CStatus | S2CEnterFloor | S2CMapData | S2CNpcList | S2CPosCorrect | S2CObjAppear | S2CObjDisappear | S2CObjMove | S2CObjTurn | S2CObjSprite | S2CNpcTalk | S2CNpcWindow | S2CShopWindow | S2CChatMsg | S2CPong | S2CBattleStart | S2CBattleTurn | S2CBattleEnd | S2CPets | S2CInventory;
export type S2CType = S2C["t"];

