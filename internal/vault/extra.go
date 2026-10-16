package vault

import "encoding/json"

// 未知字段保留：新版本写入的字段在旧版本读写一次后不会丢失。

type entryAlias Entry

var entryKnown = []string{"id", "name", "username", "password", "url", "notes", "tags", "totp_secret", "created_at", "updated_at"}

func (e *Entry) UnmarshalJSON(b []byte) error {
	var a entryAlias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	extra, err := extractExtra(b, entryKnown)
	if err != nil {
		return err
	}
	a.Extra = extra
	*e = Entry(a)
	return nil
}

func (e Entry) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(entryAlias(e))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, e.Extra)
}

type plainAlias plainData

func (p *plainData) UnmarshalJSON(b []byte) error {
	var a plainAlias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	extra, err := extractExtra(b, []string{"version", "entries"})
	if err != nil {
		return err
	}
	a.Extra = extra
	*p = plainData(a)
	return nil
}

func (p plainData) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(plainAlias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

func extractExtra(b []byte, known []string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for _, k := range known {
		delete(m, k)
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

func mergeExtra(b []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return b, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}
	return json.Marshal(m)
}
