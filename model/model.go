package model

import (
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mssql"    // MSSQL driver
	_ "github.com/jinzhu/gorm/dialects/mysql"    // MySQL driver
	_ "github.com/jinzhu/gorm/dialects/postgres" // Postgres driver
	_ "github.com/jinzhu/gorm/dialects/sqlite"   // SQLite driver
	"github.com/sfreiberg/go-smtpd/smtpd"
	"strings"
	"time"
)

// Config application config
var Config struct {
	QueueDbDialect        string   `toml:"QueueDbDialect"`
	QueueDbConnect        string   `toml:"QueueDbConnect"`
	StatusDbDialect       string   `toml:"StatusDbDialect"`
	StatusDbConnect       string   `toml:"StatusDbConnect"`
	Hostname              string   `toml:"Hostname"`
	AllowIP               []string `toml:"AllowIP"`
	AllowDomains          []string `toml:"AllowDomains"`
	SMTPListenAddr        string   `toml:"SMTPListenAddr"`
	MaxRepeatSend         uint     `toml:"MaxRepeatSend"`
	RepeatIntervalMinutes uint     `toml:"RepeatIntervalMinutes"`
	SendStream            uint     `toml:"SendStream"`
	ResendStream          uint     `toml:"ResendStream"`
	Debug                 bool     `toml:"Debug"`
}

var (
	// QueueDb queue db connection
	QueueDb *gorm.DB
	// StatusDb status db connection
	StatusDb *gorm.DB
)

// Queue queue email model
type Queue struct {
	ID           uint `gorm:"primary_key"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	MessageType  string
	MessageID    string
	From         string
	FromHostname string
	Rcpt         string
	RcptHostname string
	Data         []byte
	Repeat       uint
	LaterStatus  string
}

type status struct {
	ID          uint `gorm:"primary_key"`
	QueuedAt    time.Time
	SendingAt   time.Time
	From        string
	Rcpt        string
	MessageType string
	MessageID   string
	Status      string
}

// OpenQueueDb open queue database
func OpenQueueDb() (err error) {
	QueueDb, err = gorm.Open(Config.QueueDbDialect, Config.QueueDbConnect)
	if err != nil {
		return
	}
	QueueDb.LogMode(Config.Debug)
	QueueDb.AutoMigrate(&Queue{})
	return
}

// OpenStatusDb open status database
func OpenStatusDb() (err error) {
	StatusDb, err = gorm.Open(Config.StatusDbDialect, Config.StatusDbConnect)
	if err != nil {
		return
	}
	StatusDb.LogMode(Config.Debug)
	StatusDb.AutoMigrate(&status{})
	return
}

// AddToQueue add email to queue
func AddToQueue(messageType, messageID string, from smtpd.MailAddress, rcpts []smtpd.MailAddress, data []byte) {
	for _, rcpt := range rcpts {
		// ToDo fix error mssql IDENTITY_INSERT
		QueueDb.Create(&Queue{
			MessageType:  messageType,
			MessageID:    messageID,
			From:         from.Email(),
			FromHostname: from.Hostname(),
			Rcpt:         rcpt.Email(),
			RcptHostname: rcpt.Hostname(),
			Data:         data,
		})
	}
}

// GetRepeatQueue get `limit` number emails for resend
func GetRepeatQueue(limit uint) []Queue {
	var emails []Queue
	QueueDb.Where("updated_at < ? AND repeat > 0", time.Now().Local().Add(-1*time.Minute*time.Duration(Config.RepeatIntervalMinutes))).
		Limit(int(limit)).
		Find(&emails)
	return emails
}

// GetNewQueue get `limit` number new emails
func GetNewQueue(limit uint) []Queue {
	var emails []Queue
	QueueDb.Where("repeat=0").
		Limit(int(limit)).
		Find(&emails)
	return emails
}

// SetStatus set email send status
func SetStatus(email *Queue) {
	email.Repeat++
	if email.Repeat >= Config.MaxRepeatSend {
		setStatus(email)
	} else {
		if strings.HasPrefix(email.LaterStatus, "4") {
			QueueDb.Model(&Queue{ID: email.ID}).UpdateColumns(Queue{Repeat: email.Repeat, LaterStatus: email.LaterStatus, UpdatedAt: time.Now()})
		} else {
			setStatus(email)
		}
	}
}

func setStatus(email *Queue) {
	//if err := StatusDb.Create(&status{
	//	QueuedAt:    email.CreatedAt,
	//	SendingAt:   time.Now(),
	//	From:        email.From,
	//	Rcpt:        email.Rcpt,
	//	MessageType: email.MessageType,
	//	MessageID:   email.MessageID,
	//	Status:      email.LaterStatus,
	//}).Error; err != nil {
	//	log.Print(err)
	//}
	// ERROR mssql: Для столбца идентификаторов таблицы "statuses" явное значение необходимо указывать в тех случаях, когда либо IDENTITY_INSERT имеет значение ON, либо когда пользователь репликации осуществляет вставку в столбец идентификаторов, отмеченный как NOT FOR REPLICATION.
	StatusDb.Exec(
		`INSERT INTO "statuses" ("queued_at","sending_at","from","rcpt","message_type","message_id","status") VALUES (?, ?, ?, ?, ?, ?, ?)`,
		email.CreatedAt,
		time.Now(),
		email.From,
		email.Rcpt,
		email.MessageType,
		email.MessageID,
		email.LaterStatus,
	)

	QueueDb.Delete(&Queue{ID: email.ID})
}
