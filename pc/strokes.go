package main

import (
	_ "embed"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// 英字の一筆書きの判定（$1 Unistroke 方式。文字の向きが大事なので回転は±15°だけ許す）。
// 見本は練習ページ（docs/handwriting）で保存した JSON。
// 組み込み: strokes.json（ビルド時に handwriting/ の最新をコピー）
// 上書き: %APPDATA%\PhoneBoard\strokes.json があればそちらを使う

//go:embed strokes.json
var strokesJSON []byte

const strokeN = 64

type strokeFile struct {
	Format        string                 `json:"format"`
	Threshold     float64                `json:"threshold"`
	DefaultShapes map[string][][]float64 `json:"defaultShapes"`
	Letters       map[string]struct {
		UseDefault *bool         `json:"useDefault"`
		Samples    [][][]float64 `json:"samples"`
	} `json:"letters"`
}

type pt struct{ x, y float64 }

type strokeTpl struct {
	key string
	p   []pt
}

type Recognizer struct {
	Threshold float64
	tpls      []strokeTpl
}

type StrokeMatch struct {
	Key  string
	Dist float64
}

func toPts(a [][]float64) []pt {
	out := make([]pt, 0, len(a))
	for _, q := range a {
		if len(q) >= 2 {
			out = append(out, pt{q[0], q[1]})
		}
	}
	return out
}

// LoadRecognizer は見本を読み込む（データフォルダの strokes.json があれば優先）。
func LoadRecognizer() *Recognizer {
	if b, err := os.ReadFile(filepath.Join(dataDir(), "strokes.json")); err == nil {
		if r, err := NewRecognizer(b); err == nil {
			return r
		}
	}
	r, err := NewRecognizer(strokesJSON)
	if err != nil {
		return &Recognizer{Threshold: 0.3}
	}
	return r
}

func NewRecognizer(b []byte) (*Recognizer, error) {
	var f strokeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	r := &Recognizer{Threshold: f.Threshold}
	if r.Threshold <= 0 {
		r.Threshold = 0.3
	}
	keys := make([]string, 0, len(f.DefaultShapes))
	for k := range f.DefaultShapes {
		keys = append(keys, k)
	}
	for k := range f.Letters {
		if _, ok := f.DefaultShapes[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		L, has := f.Letters[k]
		useDef := !has || L.UseDefault == nil || *L.UseDefault
		if d, ok := f.DefaultShapes[k]; ok && useDef {
			if p := toPts(d); len(p) >= 2 {
				r.tpls = append(r.tpls, strokeTpl{k, normalizeStroke(p)})
			}
		}
		for _, s := range L.Samples {
			// 保存された見本は正規化済み（64点）だが、念のため同じ処理を通す
			if p := toPts(s); len(p) >= 2 {
				r.tpls = append(r.tpls, strokeTpl{k, normalizeStroke(p)})
			}
		}
	}
	return r, nil
}

func pathLen(p []pt) float64 {
	d := 0.0
	for i := 1; i < len(p); i++ {
		d += math.Hypot(p[i].x-p[i-1].x, p[i].y-p[i-1].y)
	}
	return d
}

func resampleStroke(pts []pt, n int) []pt {
	src := append([]pt{}, pts...)
	I := pathLen(src) / float64(n-1)
	out := []pt{src[0]}
	if I == 0 {
		for len(out) < n {
			out = append(out, src[0])
		}
		return out
	}
	D := 0.0
	for i := 1; i < len(src); i++ {
		d := math.Hypot(src[i].x-src[i-1].x, src[i].y-src[i-1].y)
		if D+d >= I && d > 0 {
			t := (I - D) / d
			q := pt{src[i-1].x + t*(src[i].x-src[i-1].x), src[i-1].y + t*(src[i].y-src[i-1].y)}
			out = append(out, q)
			src = append(src[:i], append([]pt{q}, src[i:]...)...)
			D = 0
		} else {
			D += d
		}
	}
	for len(out) < n {
		out = append(out, src[len(src)-1])
	}
	return out[:n]
}

// normalizeStroke: 64点に並べ直し、重心を原点に、縦横比を保って大きさをそろえる。
func normalizeStroke(pts []pt) []pt {
	r := resampleStroke(pts, strokeN)
	cx, cy := 0.0, 0.0
	x0, x1, y0, y1 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, p := range r {
		cx += p.x
		cy += p.y
		x0, x1 = math.Min(x0, p.x), math.Max(x1, p.x)
		y0, y1 = math.Min(y0, p.y), math.Max(y1, p.y)
	}
	cx /= strokeN
	cy /= strokeN
	s := math.Max(x1-x0, y1-y0)
	if s == 0 {
		s = 1
	}
	out := make([]pt, strokeN)
	for i, p := range r {
		out[i] = pt{(p.x - cx) / s, (p.y - cy) / s}
	}
	return out
}

func rotatePts(p []pt, a float64) []pt {
	c, s := math.Cos(a), math.Sin(a)
	out := make([]pt, len(p))
	for i, q := range p {
		out[i] = pt{q.x*c - q.y*s, q.x*s + q.y*c}
	}
	return out
}

func strokeDist(a, b []pt) float64 {
	d := 0.0
	for i := 0; i < strokeN; i++ {
		d += math.Hypot(a[i].x-b[i].x, a[i].y-b[i].y)
	}
	return d / strokeN
}

var strokeAngles = []float64{-0.26, -0.13, 0, 0.13, 0.26}

// Recognize は近い順の候補（文字ごとに一番近い見本の距離）を返す。
func (r *Recognizer) Recognize(raw []pt) []StrokeMatch {
	if len(raw) < 2 || len(r.tpls) == 0 {
		return nil
	}
	c := normalizeStroke(raw)
	rots := make([][]pt, len(strokeAngles))
	for i, a := range strokeAngles {
		rots[i] = rotatePts(c, a)
	}
	best := map[string]float64{}
	for _, t := range r.tpls {
		d := math.Inf(1)
		for _, q := range rots {
			d = math.Min(d, strokeDist(q, t.p))
		}
		if b, ok := best[t.key]; !ok || d < b {
			best[t.key] = d
		}
	}
	out := make([]StrokeMatch, 0, len(best))
	for k, d := range best {
		out = append(out, StrokeMatch{k, d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dist < out[j].Dist })
	return out
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
