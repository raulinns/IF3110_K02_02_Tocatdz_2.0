package schemadiff

import (
	"context"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

const (
	CompareTimeout   = 10 * time.Second
	MaxSchemaBytes   = 63
	MaxTables        = 2000
	MaxObjects       = 50000
	MaxGeneratedSQL  = 512 << 10
	MaxDisplayChange = 2000
)

// Nama yang sudah di-quote berasal dari quote_ident di database supaya aturan keyword PostgreSQL ikut terpakai.
type Snapshot struct {
	Schema string
	Quoted string
	Tables []Table
	Views  []View
}

type Table struct {
	Name        string
	Quoted      string
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}

// Serial berarti default berasal dari sequence milik kolom; default nextval tidak dibandingkan.
type Column struct {
	Name      string
	Quoted    string
	Type      string
	NotNull   bool
	Default   *string
	Identity  string
	Generated string
	Serial    bool
}

type Constraint struct {
	Name       string
	Quoted     string
	Kind       string
	Definition string
}

// Tail adalah bagian definisi index mulai dari USING, sehingga index bisa dibuat ulang di schema lain.
type Index struct {
	Name   string
	Quoted string
	Unique bool
	Tail   string
}

type View struct {
	Name       string
	Quoted     string
	Definition string
}

type ChangeAction string

const (
	ActionAdd   ChangeAction = "add"
	ActionDrop  ChangeAction = "drop"
	ActionAlter ChangeAction = "alter"
)

func (a ChangeAction) Label() string {
	switch a {
	case ActionAdd:
		return "Tambah"
	case ActionDrop:
		return "Hapus"
	}
	return "Ubah"
}

type Change struct {
	Action      ChangeAction
	Object      string
	Name        string
	Before      string
	After       string
	Destructive bool
}

type Result struct {
	Current     Snapshot
	Desired     Snapshot
	Changes     []Change
	Warnings    []string
	SQL         security.Secret
	Validation  string
	Destructive int
}

type Request struct {
	Principal     security.Principal
	CurrentID     security.ID
	CurrentSchema string
	DesiredID     security.ID
	DesiredSchema string
}

type Reader interface {
	Snapshot(context.Context, security.ID, string) (Snapshot, error)
}

// Kandidat SQL melewati aturan validator migration yang sama sebelum ditawarkan sebagai draft.
type Validator interface {
	QualifyFunctions(context.Context, security.Secret) (security.Secret, error)
	Validate(context.Context, security.Secret) error
}

type Application interface {
	Compare(context.Context, Request) (Result, error)
}
