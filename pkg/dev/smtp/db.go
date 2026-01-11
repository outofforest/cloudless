package smtp

import (
	"io"
	"slices"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/backendutil"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/pkg/errors"
)

const inbox = "INBOX"

type db struct {
	mu    sync.Mutex
	users map[string]*user
}

func newDB() *db {
	return &db{
		users: map[string]*user{},
	}
}

func (db *db) User(username string, create bool) *user {
	db.mu.Lock()
	defer db.mu.Unlock()

	u, ok := db.users[username]
	if !ok {
		u = newUser(username, &db.mu)
		if create {
			db.users[username] = u
		}
	}
	return u
}

func newUser(username string, mu *sync.Mutex) *user {
	u := &user{
		mu:       mu,
		username: username,
	}
	u.mailboxes = map[string]*mailbox{
		inbox: {
			name: inbox,
			user: u,
		},
	}
	return u
}

type user struct {
	mu *sync.Mutex

	username  string
	mailboxes map[string]*mailbox
}

func (u *user) Username() string {
	return u.username
}

func (u *user) Inbox() *mailbox {
	return u.mailboxes[inbox]
}

func (u *user) ListMailboxes(subscribed bool) ([]backend.Mailbox, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	mailboxes := make([]backend.Mailbox, 0, len(u.mailboxes))
	for _, mailbox := range u.mailboxes {
		if subscribed && !mailbox.Subscribed {
			continue
		}

		mailboxes = append(mailboxes, mailbox)
	}
	return mailboxes, nil
}

func (u *user) GetMailbox(name string) (backend.Mailbox, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	mailbox, ok := u.mailboxes[name]
	if !ok {
		return nil, errors.New("no such mailbox")
	}
	return mailbox, nil
}

func (u *user) CreateMailbox(name string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if _, ok := u.mailboxes[name]; ok {
		return errors.New("mailbox already exists")
	}

	u.mailboxes[name] = &mailbox{name: name, user: u}
	return nil
}

func (u *user) DeleteMailbox(name string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if name == inbox {
		return errors.New("cannot delete inbox")
	}
	if _, ok := u.mailboxes[name]; !ok {
		return errors.New("no such mailbox")
	}

	delete(u.mailboxes, name)
	return nil
}

func (u *user) RenameMailbox(existingName, newName string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	mbox, ok := u.mailboxes[existingName]
	if !ok {
		return errors.New("no such mailbox")
	}

	u.mailboxes[newName] = &mailbox{
		name:     newName,
		Messages: mbox.Messages,
		user:     u,
	}

	mbox.Messages = nil

	if existingName != inbox {
		delete(u.mailboxes, existingName)
	}

	return nil
}

func (u *user) Logout() error {
	return nil
}

type mailbox struct {
	Subscribed bool
	Messages   []*memory.Message

	name string
	user *user
}

func (m *mailbox) Name() string {
	return m.name
}

func (m *mailbox) Info() (*imap.MailboxInfo, error) {
	info := &imap.MailboxInfo{
		Delimiter: memory.Delimiter,
		Name:      m.name,
	}
	return info, nil
}

func (m *mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	status := imap.NewMailboxStatus(m.name, items)
	status.Flags = m.flags()
	status.PermanentFlags = []string{"\\*"}
	status.UnseenSeqNum = m.unseenSeqNum()

	for _, name := range items {
		switch name {
		case imap.StatusMessages:
			status.Messages = uint32(len(m.Messages))
		case imap.StatusUidNext:
			status.UidNext = m.uidNext()
		case imap.StatusUidValidity:
			status.UidValidity = 1
		case imap.StatusRecent:
			status.Recent = 0 // TODO
		case imap.StatusUnseen:
			status.Unseen = 0 // TODO
		}
	}

	return status, nil
}

func (m *mailbox) SetSubscribed(subscribed bool) error {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	m.Subscribed = subscribed
	return nil
}

func (m *mailbox) Check() error {
	return nil
}

func (m *mailbox) ListMessages(uid bool, seqSet *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	defer close(ch)

	for i, msg := range m.Messages {
		seqNum := uint32(i + 1)

		var id uint32
		if uid {
			id = msg.Uid
		} else {
			id = seqNum
		}
		if !seqSet.Contains(id) {
			continue
		}

		m, err := msg.Fetch(seqNum, items)
		if err != nil {
			continue
		}

		ch <- m
	}

	return nil
}

func (m *mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	var ids []uint32
	for i, msg := range m.Messages {
		seqNum := uint32(i + 1)

		ok, err := msg.Match(seqNum, criteria)
		if err != nil || !ok {
			continue
		}

		var id uint32
		if uid {
			id = msg.Uid
		} else {
			id = seqNum
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	if date.IsZero() {
		date = time.Now()
	}

	b, err := io.ReadAll(body)
	if err != nil {
		return errors.WithStack(err)
	}

	m.Messages = append(m.Messages, &memory.Message{
		Uid:   m.uidNext(),
		Date:  date,
		Size:  uint32(len(b)),
		Flags: flags,
		Body:  b,
	})
	return nil
}

func (m *mailbox) UpdateMessagesFlags(uid bool, seqset *imap.SeqSet, op imap.FlagsOp, flags []string) error {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	for i, msg := range m.Messages {
		var id uint32
		if uid {
			id = msg.Uid
		} else {
			id = uint32(i + 1)
		}
		if !seqset.Contains(id) {
			continue
		}

		msg.Flags = backendutil.UpdateFlags(msg.Flags, op, flags)
	}

	return nil
}

func (m *mailbox) CopyMessages(uid bool, seqset *imap.SeqSet, destName string) error {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	dest, ok := m.user.mailboxes[destName]
	if !ok {
		return backend.ErrNoSuchMailbox
	}

	for i, msg := range m.Messages {
		var id uint32
		if uid {
			id = msg.Uid
		} else {
			id = uint32(i + 1)
		}
		if !seqset.Contains(id) {
			continue
		}

		msgCopy := *msg
		msgCopy.Uid = dest.uidNext()
		dest.Messages = append(dest.Messages, &msgCopy)
	}

	return nil
}

func (m *mailbox) Expunge() error {
	m.user.mu.Lock()
	defer m.user.mu.Unlock()

	for i := len(m.Messages) - 1; i >= 0; i-- {
		msg := m.Messages[i]

		if slices.Contains(msg.Flags, imap.DeletedFlag) {
			m.Messages = append(m.Messages[:i], m.Messages[i+1:]...)
		}
	}

	return nil
}

func (m *mailbox) uidNext() uint32 {
	var uid uint32
	for _, msg := range m.Messages {
		if msg.Uid > uid {
			uid = msg.Uid
		}
	}
	uid++
	return uid
}

func (m *mailbox) flags() []string {
	flagsMap := make(map[string]bool)
	for _, msg := range m.Messages {
		for _, f := range msg.Flags {
			if !flagsMap[f] {
				flagsMap[f] = true
			}
		}
	}

	var flags []string
	for f := range flagsMap {
		flags = append(flags, f)
	}
	return flags
}

func (m *mailbox) unseenSeqNum() uint32 {
	for i, msg := range m.Messages {
		seqNum := uint32(i + 1)

		if !slices.Contains(msg.Flags, imap.SeenFlag) {
			return seqNum
		}
	}
	return 0
}
