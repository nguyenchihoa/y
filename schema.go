package y

import (
	"log"
	"reflect"
	"strings"
	"sync"
)

const _version = "_version"

type fieldopts struct {
	autoincr bool
}

type field struct {
	reflect.StructField
	opts *fieldopts
}

type xinfo struct {
	pk  []string
	idx map[string]int
}

func (x *xinfo) addpk(col string) {
	x.pk = append(x.pk, col)
	if len(x.pk) == 1 {
		x.addx(col)
	}
}

func (x *xinfo) addx(col string) {
	x.idx[col] = 1
}

func newxinfo() *xinfo {
	return &xinfo{
		idx: make(map[string]int),
	}
}

type fseq []string

func (f fseq) alias(s string) []string {
	a := make([]string, len(f))
	for i, v := range f {
		a[i] = s + "." + v
	}
	return a
}

type fkopt struct {
	target string
	from   string
}

func (f fkopt) flip() *fkopt {
	return &fkopt{f.from, f.target}
}

type schema struct {
	t      reflect.Type
	table  string
	fields map[string]*field
	fseq   fseq
	xinfo  *xinfo
	fks    map[string]*fkopt
}

func (s *schema) parseopts(xopts []string) *fieldopts {
	opts := new(fieldopts)
	for _, opt := range xopts {
		switch opt {
		// parse lonely options
		case "pk":
			s.xinfo.addpk(xopts[0])
		case "autoincr":
			opts.autoincr = true
		// parse extended options
		default:
			ext := strings.Split(opt, ":")
			switch ext[0] {
			case "fk":
				var (
					fk     string
					fkopts []string
				)
				// has explicit fk?
				if len(ext) > 1 {
					fk = ext[1]
					fkopts = strings.Split(fk, ".")
				} else {
					fk = xopts[0]
					fkopts = strings.Split(fk, "_")
				}
				if len(fkopts) != 2 {
					log.Panicf("y/schema: Couldn't parse foreign key from \"%s\".", fk)
				}
				s.fks[fkopts[0]] = &fkopt{
					target: fkopts[1],
					from:   xopts[0],
				}
				s.xinfo.addx(xopts[0])
			}
		}
	}
	return opts
}

func (s *schema) parseName(t reflect.Type) {
	s.table = underscore(t.Name())
}

func (s *schema) parseFields(t reflect.Type) {
	for i, l := 0, t.NumField(); i < l; i++ {
		f := t.Field(i)
		col := f.Tag.Get("db")
		if col == "-" {
			continue
		}
		if f.Anonymous {
			if f.Type.Kind() != reflect.Struct {
				panic("y/schema: Y supports embedded struct only.")
			}
			s.parseFields(f.Type)
			continue
		}
		xopts := strings.Split(col, ",")
		if xopts[0] == "" {
			xopts[0] = underscore(f.Name)
		}
		s.fseq = append(s.fseq, xopts[0])
		s.fields[xopts[0]] = &field{
			f,
			s.parseopts(xopts),
		}
	}
}

func (s *schema) ptrs() []interface{} {
	return make([]interface{}, len(s.fseq))
}

func (s *schema) set(ptrs []interface{}, v value) {
	for i, col := range s.fseq {
		x := v.field(s.fields[col].Name).Addr()
		ptrs[i] = x.Interface()
	}
}

func (s *schema) create() reflect.Value {
	return reflect.New(s.t)
}

func (s *schema) parse() {
	s.parseName(s.t)
	s.parseFields(s.t)
}

func (s *schema) fk(in *schema) *fkopt {
	// forward
	fk, ok := s.fks[in.table]
	if ok {
		return fk
	}
	// reverse
	fk, ok = in.fks[s.table]
	if !ok {
		log.Panicf(
			"y/schema: The foreign key between \"%s\" and \"%s\" not found",
			s.table, in.table)
	}
	return fk.flip()
}

type cache struct {
	types map[reflect.Type]*schema
	sync.RWMutex
}

var loaded = cache{
	types: make(map[reflect.Type]*schema),
}

func newSchema(t reflect.Type) *schema {
	s := &schema{
		t:      t,
		fields: make(map[string]*field),
		xinfo:  newxinfo(),
		fks:    make(map[string]*fkopt),
	}
	s.parse()
	return s
}

func loadSchema(t reflect.Type) *schema {
	if t.Kind() != reflect.Struct {
		log.Panicln("y/schema: Y supports Struct type only.")
	}

	loaded.RLock()
	s, found := loaded.types[t]
	loaded.RUnlock()
	if found {
		return s
	}

	loaded.Lock()
	defer loaded.Unlock()
	s = newSchema(t)
	loaded.types[t] = s
	return s
}
