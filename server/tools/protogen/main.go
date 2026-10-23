// protogen 由 proto/messages.json 生成 Go 与 TypeScript 的消息定义。
//
//	go run ./tools/protogen            （在 h5/server 下运行）
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type field struct{ Name, Type string }
type def struct {
	Name   string
	Fields []field
}

type schema struct {
	Types, C2S, S2C []def
}

func main() {
	root := filepath.Join("..")
	raw, err := os.ReadFile(filepath.Join(root, "proto", "messages.json"))
	check(err)
	s := parse(raw)
	goSrc, err := format.Source(genGo(s))
	check(err)
	check(os.WriteFile(filepath.Join("internal", "proto", "messages_gen.go"), goSrc, 0o644))
	check(os.WriteFile(filepath.Join(root, "client", "src", "proto.gen.ts"), genTS(s), 0o644))
	fmt.Printf("已生成：%d 个类型，%d 条客户端消息，%d 条服务端消息\n", len(s.Types), len(s.C2S), len(s.S2C))
}

// parse 保留 JSON 中的书写顺序（map 会丢失顺序）。
func parse(raw []byte) schema {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var s schema
	expect(dec, json.Delim('{'))
	for dec.More() {
		key := str(dec)
		switch key {
		case "types", "c2s", "s2c":
			var defs []def
			expect(dec, json.Delim('{'))
			for dec.More() {
				name := str(dec)
				var pairs [][2]string
				check(dec.Decode(&pairs))
				d := def{Name: name}
				for _, p := range pairs {
					d.Fields = append(d.Fields, field{p[0], p[1]})
				}
				defs = append(defs, d)
			}
			expect(dec, json.Delim('}'))
			switch key {
			case "types":
				s.Types = defs
			case "c2s":
				s.C2S = defs
			default:
				s.S2C = defs
			}
		default:
			var skip any
			check(dec.Decode(&skip))
		}
	}
	return s
}

func genGo(s schema) []byte {
	var b bytes.Buffer
	b.WriteString("// 由 tools/protogen 根据 proto/messages.json 生成，请勿手改。\n\npackage proto\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n)\n\n")
	for _, d := range s.Types {
		goStruct(&b, d)
	}
	for _, d := range append(append([]def{}, s.C2S...), s.S2C...) {
		goStruct(&b, d)
		fmt.Fprintf(&b, "func (*%s) MsgType() string { return %q }\n\n", d.Name, d.Name)
	}
	b.WriteString("// Message 是所有消息共有的接口。\ntype Message interface{ MsgType() string }\n\n")
	b.WriteString("// DecodeClient 解析客户端发来的消息；未定义的类型返回错误。\nfunc DecodeClient(data []byte) (Message, error) {\n")
	b.WriteString("\tvar head struct{ T string `json:\"t\"` }\n\tif err := json.Unmarshal(data, &head); err != nil {\n\t\treturn nil, err\n\t}\n\tvar m Message\n\tswitch head.T {\n")
	for _, d := range s.C2S {
		fmt.Fprintf(&b, "\tcase %q:\n\t\tm = &%s{}\n", d.Name, d.Name)
	}
	b.WriteString("\tdefault:\n\t\treturn nil, fmt.Errorf(\"未知消息类型 %q\", head.T)\n\t}\n\tif err := json.Unmarshal(data, m); err != nil {\n\t\treturn nil, err\n\t}\n\treturn m, nil\n}\n\n")
	b.WriteString("// Encode 序列化消息并加上类型字段 t。\nfunc Encode(m Message) ([]byte, error) {\n")
	b.WriteString("\tbody, err := json.Marshal(m)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\thead := fmt.Sprintf(`{\"t\":%q`, m.MsgType())\n\tif len(body) > 2 {\n\t\treturn append([]byte(head+\",\"), body[1:]...), nil\n\t}\n\treturn []byte(head + \"}\"), nil\n}\n")
	return b.Bytes()
}

func goStruct(b *bytes.Buffer, d def) {
	fmt.Fprintf(b, "type %s struct {\n", d.Name)
	for _, f := range d.Fields {
		typ, opt := goType(f.Type)
		tag := f.Name
		// 零值（0、""、false、空阵列）不送，省流量；客户端 proto.gen.ts 的 fillDefaults 照 schema 补回来，
		// 所以收到的物件和以前一样每个栏位都在。巢状的结构（非指标）omitempty 不会省略，照样送。
		base := strings.TrimSuffix(f.Type, "?")
		if opt || isBuiltin(strings.TrimSuffix(base, "[]")) || strings.HasSuffix(base, "[]") {
			tag += ",omitempty"
		}
		fmt.Fprintf(b, "\t%s %s `json:%q`\n", exported(f.Name), typ, tag)
	}
	b.WriteString("}\n\n")
}

func goType(t string) (string, bool) {
	opt := strings.HasSuffix(t, "?")
	t = strings.TrimSuffix(t, "?")
	arr := strings.HasSuffix(t, "[]")
	t = strings.TrimSuffix(t, "[]")
	switch t {
	case "int":
		t = "int"
	case "string":
		t = "string"
	case "bool":
		t = "bool"
	}
	if arr {
		return "[]" + t, opt
	}
	if opt && !isBuiltin(t) {
		return "*" + t, true
	}
	return t, opt
}

func genTS(s schema) []byte {
	var b bytes.Buffer
	b.WriteString("// 由 server/tools/protogen 根据 proto/messages.json 生成，请勿手改。\n\n")
	for _, d := range s.Types {
		fmt.Fprintf(&b, "export interface %s {%s }\n", d.Name, tsFields(d.Fields))
	}
	b.WriteString("\n")
	for _, group := range []struct {
		name string
		defs []def
	}{{"C2S", s.C2S}, {"S2C", s.S2C}} {
		for _, d := range group.defs {
			fmt.Fprintf(&b, "export interface %s%s { t: %q;%s }\n", group.name, d.Name, d.Name, tsFields(d.Fields))
		}
		names := make([]string, len(group.defs))
		for i, d := range group.defs {
			names[i] = group.name + d.Name
		}
		fmt.Fprintf(&b, "export type %s = %s;\n", group.name, strings.Join(names, " | "))
		fmt.Fprintf(&b, "export type %sType = %s[\"t\"];\n\n", group.name, group.name)
	}
	// 服务端把零值省略（omitempty），收到后照这张表补回：i 数字 0、s 字串 ""、b false、a 阵列 []、T:型别 巢状、A:型别 巢状阵列；
	// 选填（?）的不补
	b.WriteString("// 栏位的预设值表（fillDefaults 用）：i 数字、s 字串、b 布林、a 阵列、T:型别、A:型别阵列\n")
	b.WriteString("const SCHEMA: Record<string, [string, string][]> = {\n")
	for _, d := range append(append([]def{}, s.Types...), s.S2C...) {
		var parts []string
		for _, f := range d.Fields {
			if strings.HasSuffix(f.Type, "?") {
				continue
			}
			t := f.Type
			arr := strings.HasSuffix(t, "[]")
			t = strings.TrimSuffix(t, "[]")
			k := ""
			switch {
			case arr && isBuiltin(t):
				k = "a"
			case arr:
				k = "A:" + t
			case t == "int":
				k = "i"
			case t == "string":
				k = "s"
			case t == "bool":
				k = "b"
			default:
				k = "T:" + t
			}
			parts = append(parts, fmt.Sprintf("[%q,%q]", f.Name, k))
		}
		fmt.Fprintf(&b, "  %s: [%s],\n", d.Name, strings.Join(parts, ","))
	}
	b.WriteString("};\n\n")
	b.WriteString(fillDefaultsTS)
	return b.Bytes()
}

const fillDefaultsTS = `/** 把服务端省略的零值补回去（就地修改，回传同一个物件）：收到的消息每个栏位都在，和服务端没省略时一样 */
export function fillDefaults<T>(o: T, type?: string): T {
  const s = SCHEMA[type ?? (o as { t: string }).t];
  if (!s || !o || typeof o !== "object") return o;
  const r = o as Record<string, unknown>;
  for (const [k, kind] of s) {
    const v = r[k];
    switch (kind) {
      case "i": if (v === undefined) r[k] = 0; break;
      case "s": if (v === undefined) r[k] = ""; break;
      case "b": if (v === undefined) r[k] = false; break;
      case "a": if (v === undefined || v === null) r[k] = []; break;
      default:
        if (kind.startsWith("A:")) { if (v === undefined || v === null) r[k] = []; else for (const x of v as unknown[]) fillDefaults(x, kind.slice(2)); }
        else if (kind.startsWith("T:") && v) fillDefaults(v, kind.slice(2));
    }
  }
  return o;
}
`

func tsFields(fs []field) string {
	var parts []string
	for _, f := range fs {
		t, name := f.Type, f.Name
		if strings.HasSuffix(t, "?") {
			t = strings.TrimSuffix(t, "?")
			name += "?"
		}
		arr := strings.HasSuffix(t, "[]")
		t = strings.TrimSuffix(t, "[]")
		switch t {
		case "int":
			t = "number"
		case "string", "bool":
			if t == "bool" {
				t = "boolean"
			}
		}
		if arr {
			t += "[]"
		}
		parts = append(parts, fmt.Sprintf(" %s: %s;", name, t))
	}
	return strings.Join(parts, "")
}

func isBuiltin(t string) bool { return t == "int" || t == "string" || t == "bool" }

// exported 转成 Go 导出名，并按 Go 习惯把 id/ids 写成 ID/IDs（id、fromId、ids → ID、FromID、IDs）。
func exported(s string) string {
	s = strings.ToUpper(s[:1]) + s[1:]
	switch {
	case s == "Ok":
		return "OK"
	case s == "Hp", s == "Dp":
		return strings.ToUpper(s)
	case s == "Ids" || strings.HasSuffix(s, "Ids"):
		return s[:len(s)-3] + "IDs"
	case s == "Id" || strings.HasSuffix(s, "Id"):
		return s[:len(s)-2] + "ID"
	}
	return s
}

func expect(dec *json.Decoder, want json.Token) {
	tok, err := dec.Token()
	check(err)
	if tok != want {
		log.Fatalf("schema 格式错误：期望 %v，得到 %v", want, tok)
	}
}

func str(dec *json.Decoder) string {
	tok, err := dec.Token()
	check(err)
	s, ok := tok.(string)
	if !ok {
		log.Fatalf("schema 格式错误：期望字符串，得到 %v", tok)
	}
	return s
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
