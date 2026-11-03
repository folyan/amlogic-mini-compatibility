// Package appinfo defines shared product text and the three user actions.
package appinfo

import "strings"

const (
	Title       = "Amlogic A113X Mini"
	ErrorPrefix = "Ошибка:"
	Description = "CLI-инструмент для прошивки Alpine Linux."
	Backup      = "backup"
	Restore     = "restore"
	Flash       = "flash"
	Stock       = "stock"
)

type Action struct{ Name, Label, Title string }

// Actions returns a fresh list so callers cannot modify the common catalogue.
func Actions() []Action {
	return []Action{
		{Backup, "Создать резервную копию", "Создание резервной копии"},
		{Restore, "Восстановить из резервной копии", "Восстановление"},
		{Flash, "Установить Alpine Linux", "Установка Alpine Linux"},
		{Stock, "Вернуть сток из папки stock", "Возврат на сток"},
	}
}
func Find(name string) (Action, bool) {
	for _, action := range Actions() {
		if action.Name == name {
			return action, true
		}
	}
	return Action{}, false
}

// CleanDisplay replaces terminal controls in external text.
func CleanDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || (r >= 127 && r < 160) {
			return '?'
		}
		return r
	}, s)
}
