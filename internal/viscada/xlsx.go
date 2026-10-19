package viscada

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
)

// SheetName is the attribute sheet of the platform's workbook.
const SheetName = "设备类属性信息"

var columns = []string{"属性ID", "属性标识", "属性名称", "标准化名称", "读写模式", "单位", "计算公式", "数据类型", "排列顺序", "模式", "默认值", "时间格式", "操作标识", "数据上报", "控制计算公式"}

const (
	colIdentifier = 1
	colName       = 2
	colAccess     = 4
)

// WriteAttributes writes the platform's attribute workbook, streaming the rows
// so 50000 attributes stay cheap. Columns without a value are blank cells,
// which the platform's import reads as empty text or zero. done reports the
// rows written; ctx cancels the write.
func WriteAttributes(ctx context.Context, out io.Writer, rows []Attribute, done func(int)) error {
	z := zip.NewWriter(out)
	z.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.BestSpeed) })
	for _, part := range []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"docProps/app.xml", appProps},
		{"docProps/core.xml", fmt.Sprintf(coreProps, time.Now().UTC().Format(time.RFC3339))},
		{"xl/workbook.xml", workbook},
		{"xl/_rels/workbook.xml.rels", workbookRels},
		{"xl/sharedStrings.xml", emptySharedStrings},
		{"xl/styles.xml", styles},
		{"xl/worksheets/sheet1.xml", notesSheet},
	} {
		w, err := z.Create(part.name)
		if err != nil {
			return err
		}
		if _, err = io.WriteString(w, part.body); err != nil {
			return err
		}
	}
	w, err := z.Create("xl/worksheets/sheet2.xml")
	if err != nil {
		return err
	}
	b := bufio.NewWriterSize(w, 256<<10)
	fmt.Fprintf(b, sheetHead, len(rows)+1)
	b.WriteString(`<row r="1">`)
	for col, title := range columns {
		fmt.Fprintf(b, `<c r="%c1" s="3" t="inlineStr"><is><t>%s</t></is></c>`, 'A'+col, title)
	}
	b.WriteString("</row>\n")
	text := func(s string) {
		b.WriteString(`" s="4" t="inlineStr"><is><t xml:space="preserve">`)
		xml.EscapeText(b, []byte(s))
		b.WriteString(`</t></is></c>`)
	}
	for i, a := range rows {
		if i%2000 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if done != nil {
				done(i)
			}
		}
		n := strconv.Itoa(i + 2)
		b.WriteString(`<row r="`)
		b.WriteString(n)
		b.WriteString(`">`)
		for col := range columns {
			b.WriteString(`<c r="`)
			b.WriteByte(byte('A' + col))
			b.WriteString(n)
			switch {
			case col == colIdentifier:
				text(a.Identifier)
			case col == colName && a.Name != "":
				text(a.Name)
			case col == colAccess && a.Access != "":
				b.WriteString(`" s="4" t="n"><v>`)
				b.WriteString(a.Access)
				b.WriteString(`</v></c>`)
			default:
				b.WriteString(`" s="4"/>`)
			}
		}
		b.WriteString("</row>\n")
	}
	b.WriteString(sheetTail)
	if err := b.Flush(); err != nil {
		return err
	}
	if done != nil {
		done(len(rows))
	}
	return z.Close()
}

// ReadAttributes reads the rows of the sheet named SheetName, else of the first
// sheet whose header has 属性标识. Sheets are streamed, so done reports the
// bytes of the sheet read and its size; ctx cancels the read.
func ReadAttributes(ctx context.Context, r io.ReaderAt, size int64, done func(read, total int64)) ([]Attribute, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, errors.New("不是有效的 xlsx 文件")
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Type   string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if err := decodePart(files, "xl/workbook.xml", &wb); err != nil {
		return nil, err
	}
	if err := decodePart(files, "xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	shared := "xl/sharedStrings.xml"
	for _, rel := range rels.Items {
		target := path.Join("xl", rel.Target)
		if strings.HasPrefix(rel.Target, "/") {
			target = strings.TrimPrefix(rel.Target, "/")
		}
		targets[rel.ID] = target
		if strings.HasSuffix(rel.Type, "/sharedStrings") {
			shared = target
		}
	}
	var strs []string
	if f := files[shared]; f != nil {
		if strs, err = sharedStrings(ctx, f); err != nil {
			return nil, err
		}
	}
	var order []*zip.File
	for _, s := range wb.Sheets {
		if f := files[targets[s.ID]]; f != nil {
			if s.Name == SheetName {
				order = append([]*zip.File{f}, order...)
			} else {
				order = append(order, f)
			}
		}
	}
	for _, f := range order {
		rows, found, err := readSheet(ctx, f, strs, done)
		if err != nil || found {
			return rows, err
		}
	}
	return nil, errors.New("未找到含“属性标识”列的工作表")
}

func decodePart(files map[string]*zip.File, name string, v any) error {
	f := files[name]
	if f == nil {
		return fmt.Errorf("xlsx 缺少 %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := xml.NewDecoder(rc).Decode(v); err != nil {
		return fmt.Errorf("%s 解析失败: %w", name, err)
	}
	return nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// tokens is a streaming tokenizer for worksheet parts, several times faster
// than encoding/xml there. It relies on what spreadsheet writers emit: no
// CDATA and no '>' inside attribute values or comments.
type tokens struct {
	r         *bufio.Reader
	text, tag []byte // character data before the current tag, and the tag
	name      string
	end, open bool // end: a closing tag; open: a self-closing tag still to close
}

func newTokens(r io.Reader) *tokens { return &tokens{r: bufio.NewReaderSize(r, 256<<10)} }

func (t *tokens) until(delim byte, dst []byte) ([]byte, error) {
	dst = dst[:0]
	for {
		chunk, err := t.r.ReadSlice(delim)
		dst = append(dst, chunk...)
		if err != bufio.ErrBufferFull {
			return dst, err
		}
	}
}

// next advances to the next start or end tag; text then holds the character
// data that preceded it, so a leaf element's text is the text at its end tag.
func (t *tokens) next() error {
	if t.open {
		t.open, t.end, t.text = false, true, t.text[:0]
		return nil
	}
	for {
		var err error
		if t.text, err = t.until('<', t.text); err != nil {
			return err
		}
		t.text = t.text[:len(t.text)-1]
		if t.tag, err = t.until('>', t.tag); err != nil {
			return io.ErrUnexpectedEOF
		}
		t.tag = t.tag[:len(t.tag)-1]
		if len(t.tag) == 0 || t.tag[0] == '?' || t.tag[0] == '!' {
			continue
		}
		t.end = t.tag[0] == '/'
		t.open = !t.end && t.tag[len(t.tag)-1] == '/'
		name := bytes.TrimLeft(t.tag, "/")
		if i := bytes.IndexAny(name, " \t\r\n/"); i >= 0 {
			name = name[:i]
		}
		if i := bytes.IndexByte(name, ':'); i >= 0 {
			name = name[i+1:]
		}
		t.name = string(name)
		return nil
	}
}

func (t *tokens) attr(name string) string {
	for tag := t.tag; ; {
		i := bytes.Index(tag, []byte(name+"="))
		if i < 0 || i+len(name)+2 > len(tag) {
			return ""
		}
		quote, rest := tag[i+len(name)+1], tag[i+len(name)+2:]
		if i > 0 && bytes.IndexByte([]byte(" \t\r\n"), tag[i-1]) >= 0 && (quote == '"' || quote == '\'') {
			if j := bytes.IndexByte(rest, quote); j >= 0 {
				return unescape(rest[:j])
			}
			return ""
		}
		tag = tag[i+len(name):]
	}
}

func unescape(b []byte) string {
	if bytes.IndexByte(b, '&') < 0 {
		return string(b)
	}
	var out strings.Builder
	for len(b) > 0 {
		i := bytes.IndexByte(b, '&')
		j := bytes.IndexByte(b[max(i, 0):], ';')
		if i < 0 || j < 0 {
			out.Write(b)
			break
		}
		out.Write(b[:i])
		entity, ok := string(b[i+1:i+j]), true
		switch {
		case entity == "lt":
			out.WriteByte('<')
		case entity == "gt":
			out.WriteByte('>')
		case entity == "amp":
			out.WriteByte('&')
		case entity == "quot":
			out.WriteByte('"')
		case entity == "apos":
			out.WriteByte('\'')
		case strings.HasPrefix(entity, "#x"), strings.HasPrefix(entity, "#X"):
			r, err := strconv.ParseUint(entity[2:], 16, 32)
			ok = err == nil
			out.WriteRune(rune(r))
		case strings.HasPrefix(entity, "#"):
			r, err := strconv.ParseUint(entity[1:], 10, 32)
			ok = err == nil
			out.WriteRune(rune(r))
		default:
			ok = false
		}
		if !ok {
			out.Write(b[i : i+j+1])
		}
		b = b[i+j+1:]
	}
	return out.String()
}

// sharedStrings reads the string table; phonetic runs (rPh) are not text.
func sharedStrings(ctx context.Context, f *zip.File) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	t := newTokens(rc)
	var out []string
	var text strings.Builder
	phonetic := 0
	for {
		if err := t.next(); err == io.EOF {
			return out, nil
		} else if err != nil {
			return nil, fmt.Errorf("共享字符串解析失败: %w", err)
		}
		switch t.name {
		case "si":
			if !t.end {
				text.Reset()
				continue
			}
			out = append(out, text.String())
			if len(out)%10000 == 0 && ctx.Err() != nil {
				return nil, ctx.Err()
			}
		case "t":
			if t.end && phonetic == 0 {
				text.WriteString(unescape(t.text))
			}
		case "rPh":
			if t.end {
				phonetic--
			} else if !t.open {
				phonetic++
			}
		}
	}
}

func columnIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

// accessCode reads 读写模式 as written by the platform (1.0) or by hand (只读).
func accessCode(s string) string {
	switch s = strings.TrimSpace(s); s {
	case "只读":
		return "1"
	case "读写":
		return "2"
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil && (v == 1 || v == 2) {
		return strconv.Itoa(int(v))
	}
	return ""
}

// readSheet streams one sheet; found reports whether it has the header.
func readSheet(ctx context.Context, f *zip.File, strs []string, done func(int64, int64)) (rows []Attribute, found bool, err error) {
	rc, err := f.Open()
	if err != nil {
		return nil, false, err
	}
	defer rc.Close()
	counter := &countingReader{r: rc}
	t := newTokens(counter)
	type cell struct {
		col  int
		text string
	}
	var row []cell
	idCol, nameCol, accessCol := -1, -1, -1
	rowNum, col, phonetic := 0, -1, 0
	var cellType, value string
	var text strings.Builder
	for {
		if err := t.next(); err == io.EOF {
			return rows, idCol >= 0, nil
		} else if err != nil {
			return nil, false, fmt.Errorf("工作表解析失败: %w", err)
		}
		if !t.end {
			switch t.name {
			case "row":
				rowNum++
				if r, e := strconv.Atoi(t.attr("r")); e == nil {
					rowNum = r
				}
				row, col = row[:0], -1
				if rowNum%2000 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, false, err
					}
					if done != nil {
						done(counter.n, int64(f.UncompressedSize64))
					}
				}
			case "c":
				col++
				if ref := t.attr("r"); ref != "" {
					col = columnIndex(ref)
				}
				cellType, value = t.attr("t"), ""
				text.Reset()
			case "rPh":
				if !t.open {
					phonetic++
				}
			}
			continue
		}
		switch t.name {
		case "v":
			value = unescape(t.text)
		case "t":
			if phonetic == 0 {
				text.WriteString(unescape(t.text))
			}
		case "rPh":
			phonetic--
		case "c":
			s := value
			switch cellType {
			case "s":
				s = ""
				if i, e := strconv.Atoi(strings.TrimSpace(value)); e == nil && i >= 0 && i < len(strs) {
					s = strs[i]
				}
			case "inlineStr":
				s = text.String()
			}
			row = append(row, cell{col, s})
		case "row":
			if idCol < 0 {
				for _, c := range row {
					switch strings.TrimSpace(c.text) {
					case "属性标识":
						idCol = c.col
					case "属性名称":
						nameCol = c.col
					case "读写模式":
						accessCol = c.col
					}
				}
				continue
			}
			a := Attribute{Row: rowNum}
			for _, c := range row {
				switch c.col {
				case idCol:
					a.Identifier = c.text
				case nameCol:
					a.Name = c.text
				case accessCol:
					a.Access = accessCode(c.text)
				}
			}
			if strings.TrimSpace(a.Identifier) != "" {
				rows = append(rows, a)
			}
		}
	}
}

// The parts below mirror the workbook the platform exports.
const (
	contentTypes       = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default ContentType="application/vnd.openxmlformats-package.relationships+xml" Extension="rels"/><Default ContentType="application/xml" Extension="xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml" PartName="/docProps/app.xml"/><Override ContentType="application/vnd.openxmlformats-package.core-properties+xml" PartName="/docProps/core.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml" PartName="/xl/sharedStrings.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml" PartName="/xl/styles.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml" PartName="/xl/workbook.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet1.xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml" PartName="/xl/worksheets/sheet2.xml"/></Types>`
	rootRels           = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="xl/workbook.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"/><Relationship Id="rId2" Target="docProps/app.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"/><Relationship Id="rId3" Target="docProps/core.xml" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"/></Relationships>`
	appProps           = `<?xml version="1.0" encoding="UTF-8"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>OPC UA AI Studio</Application></Properties>`
	coreProps          = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created><dc:creator>OPC UA AI Studio</dc:creator></cp:coreProperties>`
	workbook           = `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><workbookPr date1904="false"/><bookViews><workbookView activeTab="1"/></bookViews><sheets><sheet name="说明" r:id="rId3" sheetId="1"/><sheet name="设备类属性信息" r:id="rId4" sheetId="2"/></sheets></workbook>`
	workbookRels       = `<?xml version="1.0" encoding="UTF-8" standalone="no"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="sharedStrings.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings"/><Relationship Id="rId2" Target="styles.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"/><Relationship Id="rId3" Target="worksheets/sheet1.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"/><Relationship Id="rId4" Target="worksheets/sheet2.xml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"/></Relationships>`
	emptySharedStrings = `<?xml version="1.0" encoding="UTF-8"?><sst count="0" uniqueCount="0" xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`
	styles             = `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="0"/><fonts count="3"><font><sz val="11.0"/><color indexed="8"/><name val="Calibri"/><family val="2"/><scheme val="minor"/></font><font><name val="Calibri"/><sz val="11.0"/></font><font><name val="宋体"/><sz val="14.0"/><b val="true"/></font></fonts><fills count="4"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="darkGray"/></fill><fill><patternFill patternType="solid"/></fill><fill><patternFill patternType="solid"><fgColor indexed="22"/></patternFill></fill></fills><borders count="5"><border><left/><right/><top/><bottom/><diagonal/></border><border><left style="thin"/></border><border><left style="thin"/><right style="thin"/></border><border><left style="thin"/><right style="thin"/><top style="thin"/></border><border><left style="thin"/><right style="thin"/><top style="thin"/><bottom style="thin"/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="6"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="true"><alignment wrapText="true" vertical="center"/></xf><xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="true"/><xf numFmtId="0" fontId="2" fillId="3" borderId="4" xfId="0" applyBorder="true" applyFill="true" applyFont="true"><alignment horizontal="center" wrapText="true" vertical="center"/><protection locked="true"/></xf><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="true"/><xf numFmtId="49" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="true" applyFont="true"/></cellXfs></styleSheet>`
	notesSheet         = `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:A1"/><sheetViews><sheetView workbookViewId="0"/></sheetViews><sheetFormatPr defaultRowHeight="15.0"/><sheetData><row r="1"><c r="A1" s="1" t="inlineStr"><is><t xml:space="preserve">导出说明：&#xa;（1）数据类型说明，整数(1)，浮点数(2)，双精度浮点数(3)，字符(4)，日期(5)，布尔(6)；&#xa;（2）读写模式说明，只读(1)，读写(2)；&#xa;（3）模式说明，真实点位(0/不填)，虚拟点位(1)&#xa;</t></is></c></row></sheetData><mergeCells count="1"><mergeCell ref="A1:P10"/></mergeCells><pageMargins bottom="0.75" footer="0.3" header="0.3" left="0.7" right="0.7" top="0.75"/></worksheet>`
	sheetHead          = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:O%d"/><sheetViews><sheetView workbookViewId="0" tabSelected="true"/></sheetViews><sheetFormatPr defaultRowHeight="15.0"/><cols><col min="2" max="2" width="60" customWidth="true"/></cols><sheetData>` + "\n"
	sheetTail          = `</sheetData><pageMargins bottom="0.75" footer="0.3" header="0.3" left="0.7" right="0.7" top="0.75"/></worksheet>`
)
