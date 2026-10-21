// Package conversion — статусы конверсий (паритет Keitaro 11: G-301/302/303, ключ дедупа G-304).
//
// Keitaro 11 breaking change: постбек без status= игнорируется.
// Системные статусы: lead, sale, rejected, registration, deposit, trash.
// trash: Revenue обнуляется, сумма сохраняется в метрике Revenue (trash).
// Кастомные статусы: [a-z0-9_]{1,32} (регистр приводится к нижнему).
package conversion

import (
	"fmt"
	"regexp"
	"strings"
)

// System statuses.
const (
	StatusLead         = "lead"
	StatusSale         = "sale"
	StatusRejected     = "rejected"
	StatusRegistration = "registration"
	StatusDeposit      = "deposit"
	StatusTrash        = "trash"
)

var systemStatuses = map[string]bool{
	StatusLead: true, StatusSale: true, StatusRejected: true,
	StatusRegistration: true, StatusDeposit: true, StatusTrash: true,
}

var customRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// ParseStatus нормализует и валидирует статус постбека.
// Возвращает ok=false для пустого/неизвестного (такой постбек игнорируется).
func ParseStatus(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	if systemStatuses[s] {
		return s, true
	}
	if customRe.MatchString(s) {
		return s, true
	}
	return "", false
}

// RevenueByStatus — вклад в метрику Revenue (trash обнуляется).
func RevenueByStatus(status string, payout float64) float64 {
	if status == StatusTrash {
		return 0
	}
	return payout
}

// TrashRevenueByStatus — вклад в метрику Revenue (trash).
func TrashRevenueByStatus(status string, payout float64) float64 {
	if status == StatusTrash {
		return payout
	}
	return 0
}

// DedupKey — ключ дедупликации: cid + tid (G-304).
// Непустой tid кодируется с длинами, поэтому NUL внутри значений
// не склеивается с чужим ключом. Пустой tid даёт голый cid
// (совместимость с записями без tid).
func DedupKey(cid, tid string) string {
	if tid == "" {
		return cid
	}
	return fmt.Sprintf("%d:%s\x00%d:%s", len(cid), cid, len(tid), tid)
}
