package database

import "time"

type User struct {
	DefaultAlbumID string    `gorm:"size:32" json:"default_album_id"`
	ID             string    `gorm:"primaryKey;size:32" json:"id"`
	Username       string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash   string    `gorm:"size:128;not null" json:"-"`
	Role           string    `gorm:"size:8;not null;default:user" json:"role"`
	Enabled        bool      `gorm:"not null;default:true" json:"enabled"`
	QuotaBytes     int64     `gorm:"not null;default:0" json:"quota_bytes"`
	UsedBytes      int64     `gorm:"not null;default:0" json:"-"`
	MaxFileBytes   int64     `gorm:"not null;default:0" json:"max_file_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `gorm:"primaryKey;size:64"`
	UserID    string    `gorm:"index;size:32;not null"`
	CSRF      string    `gorm:"size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

type Album struct {
	RefererMode       string    `gorm:"size:16;not null;default:inherit" json:"referer_mode"`
	Referers          string    `gorm:"type:text" json:"-"`
	AllowEmptyReferer bool      `gorm:"not null;default:true" json:"allow_empty_referer"`
	ID                string    `gorm:"primaryKey;size:32" json:"id"`
	UserID            string    `gorm:"index;size:32;not null" json:"-"`
	Name              string    `gorm:"size:100;not null" json:"name"`
	GuestVisible      bool      `gorm:"not null;default:false" json:"guest_visible"`
	CreatedAt         time.Time `json:"created_at"`
}

type AlbumMember struct {
	AlbumID string `gorm:"primaryKey;size:32"`
	UserID  string `gorm:"primaryKey;size:32"`
}

// UploadReservation 在文件落盘前预占容量，避免两个并发请求同时通过余额检查。
type UploadReservation struct {
	ID     string `gorm:"primaryKey;size:32"`
	UserID string `gorm:"index;size:32;not null"`
	Bytes  int64  `gorm:"not null"`
}

type Share struct {
	UserID       string     `gorm:"index;size:32" json:"owner_id"`
	ID           string     `gorm:"primaryKey;size:32" json:"id"`
	Kind         string     `gorm:"size:8;not null" json:"kind"`
	TargetID     string     `gorm:"index;size:32;not null" json:"target_id"`
	TokenHash    string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	TokenCipher  string     `gorm:"type:text;not null" json:"-"`
	PasswordHash string     `gorm:"size:128" json:"-"`
	ExpiresAt    *time.Time `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type ShareSession struct {
	ID        string    `gorm:"primaryKey;size:64"`
	ShareID   string    `gorm:"index;size:32;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

type Image struct {
	ID                string    `gorm:"primaryKey;size:32;index:,composite:owner_ready_date,priority:4;index:,composite:album_ready_date,priority:4;index:,composite:ready_date,priority:3" json:"id"`
	UserID            string    `gorm:"index:,composite:owner_date,priority:1;index:,composite:owner_ready_date,priority:1;size:32;not null" json:"-"`
	AlbumID           *string   `gorm:"index;index:,composite:album_ready_date,priority:1;size:32" json:"album_id"`
	AlbumRef          *Album    `gorm:"foreignKey:AlbumID;references:ID;constraint:OnDelete:SET NULL" json:"-"`
	Name              string    `gorm:"size:255;not null" json:"name"`
	StorageID         string    `gorm:"index;size:32;not null" json:"storage_id"`
	Key               string    `gorm:"size:255;not null" json:"-"`
	ThumbKey          string    `gorm:"size:255" json:"-"`
	PreviewKey        string    `gorm:"size:255" json:"-"`
	Format            string    `gorm:"size:10;not null" json:"format"`
	Size              int64     `gorm:"not null" json:"size"`
	OriginalSize      int64     `gorm:"not null;default:0" json:"original_size"`
	Optimized         bool      `gorm:"not null;default:false" json:"optimized"`
	CompressionStatus string    `gorm:"size:32;not null;default:original" json:"compression_status"`
	Width             int       `gorm:"not null" json:"width"`
	Height            int       `gorm:"not null" json:"height"`
	State             string    `gorm:"index;index:,composite:owner_ready_date,priority:2;index:,composite:album_ready_date,priority:2;index:,composite:ready_date,priority:1;size:16;not null" json:"-"`
	CreatedAt         time.Time `gorm:"index:,composite:owner_date,priority:2;index:,composite:owner_ready_date,priority:3;index:,composite:album_ready_date,priority:3;index:,composite:ready_date,priority:2" json:"created_at"`
}

type StorageProfile struct {
	ID          string    `gorm:"primaryKey;size:32" json:"id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Kind        string    `gorm:"size:8;not null" json:"kind"`
	Enabled     bool      `gorm:"not null" json:"enabled"`
	Root        string    `gorm:"size:2048" json:"root"`
	Endpoint    string    `gorm:"size:2048" json:"endpoint"`
	Region      string    `gorm:"size:128" json:"region"`
	Bucket      string    `gorm:"size:255" json:"bucket"`
	Prefix      string    `gorm:"size:512" json:"prefix"`
	PathStyle   bool      `gorm:"not null" json:"path_style"`
	Credentials string    `gorm:"type:text" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AccessRule struct {
	Scope             string `gorm:"primaryKey;size:16" json:"scope"`
	IPEnabled         bool   `gorm:"not null" json:"ip_enabled"`
	IPs               string `gorm:"type:text;not null" json:"-"`
	RefererEnabled    bool   `gorm:"not null" json:"referer_enabled"`
	Referers          string `gorm:"type:text;not null" json:"-"`
	AllowEmptyReferer bool   `gorm:"not null" json:"allow_empty_referer"`
}

type APIToken struct {
	Authorization string     `gorm:"type:text" json:"-"`
	LastUsedAt    *time.Time `json:"last_used_at"`
	RevokedAt     *time.Time `gorm:"index" json:"revoked_at"`
	ID            string     `gorm:"primaryKey;size:32" json:"id"`
	UserID        string     `gorm:"index;size:32;not null" json:"-"`
	Name          string     `gorm:"size:64;not null" json:"name"`
	Hash          string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	Prefix        string     `gorm:"size:16;not null" json:"prefix"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `gorm:"index" json:"expires_at"`
}

type Setting struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string `gorm:"size:2048;not null"`
}

type SchemaMigration struct {
	Version   int `gorm:"primaryKey;autoIncrement:false"`
	AppliedAt time.Time
}
