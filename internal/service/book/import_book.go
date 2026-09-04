package book

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"connectrpc.com/connect"
	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/customfields"
	"github.com/0utl1er-tech/phox-customer/internal/search"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func (s *BookService) ImportBook(ctx context.Context, req *connect.Request[bookv1.ImportBookRequest]) (*connect.Response[bookv1.ImportBookResponse], error) {
	if req.Msg.GetFileName() == "" || req.Msg.GetFileContent() == nil || req.Msg.GetOwnerId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("file_name, file_content, and owner_id are required"))
	}

	// Create a new book
	bookID := uuid.New()
	bookName := strings.TrimSuffix(req.Msg.GetFileName(), ".csv") // Use filename as book name
	createBookArgs := db.CreateBookParams{
		ID:   bookID,
		Name: bookName,
	}
	_, err := s.queries.CreateBook(ctx, createBookArgs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create book: %v", err))
	}

	// Create a permit for the owner
	permitID := uuid.New()
	createPermitArgs := db.CreatePermitParams{
		ID:     permitID,
		BookID: bookID,
		UserID: req.Msg.GetOwnerId(),
		Role:   db.RoleOwner, // Assuming 'owner' role for the uploader
	}
	_, err = s.queries.CreatePermit(ctx, createPermitArgs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create permit for owner: %v", err))
	}

	// Seed default Status ("未対応") — 詳細は create_book.go と同じ。
	if err := SeedDefaultStatus(ctx, s.queries, bookID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to seed default status: %v", err))
	}

	// Parse CSV content
	reader := csv.NewReader(strings.NewReader(string(req.Msg.GetFileContent())))
	header, err := reader.Read()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to read CSV header: %v", err))
	}

	// Map CSV headers to database columns
	columnMap := make(map[string]int)
	for i, col := range header {
		columnMap[strings.ToLower(strings.TrimSpace(col))] = i
	}

	// Expected columns
	// The user can provide an 'id' column, otherwise it will be auto-generated.
	// Other columns are optional and will default to empty string if not present.
	idCol := -1
	if idx, ok := columnMap["id"]; ok {
		idCol = idx
	}
	phoneCol := -1
	if idx, ok := columnMap["phone"]; ok {
		phoneCol = idx
	}
	categoryCol := -1
	if idx, ok := columnMap["category"]; ok {
		categoryCol = idx
	}
	nameCol := -1
	if idx, ok := columnMap["name"]; ok {
		nameCol = idx
	}
	corporationCol := -1
	if idx, ok := columnMap["corporation"]; ok {
		corporationCol = idx
	}
	addressCol := -1
	if idx, ok := columnMap["address"]; ok {
		addressCol = idx
	}
	memoCol := -1
	if idx, ok := columnMap["memo"]; ok {
		memoCol = idx
	}
	// mail / email どちらの列名でも受ける
	mailCol := -1
	if idx, ok := columnMap["mail"]; ok {
		mailCol = idx
	} else if idx, ok := columnMap["email"]; ok {
		mailCol = idx
	}

	// Phase 29b: canonical 以外の列は「顧客ごとの任意差し込み変数」として
	// custom_fields に入れる (以前は黙って捨てていた)。列名を正規化した
	// キーで保存し、キャンペーン本文から {{fields.<キー>}} で参照できる。
	// 例: meo_score / meo_issues 列を持つ CSV → 1 通ごとに違う診断結果。
	customCols := buildCustomFieldColumns(header)

	// Keep the original CSV line number with each parsed row so that
	// insert-stage errors report the actual line even when earlier rows
	// were skipped during parsing.
	type customerRow struct {
		params  db.CreateCustomerParams
		lineNum int32
	}

	var customersToInsert []customerRow
	var importErrors []*bookv1.ImportError
	lineNum := 1 // Header is line 1, data starts from line 2

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			importErrors = append(importErrors, &bookv1.ImportError{
				LineNumber:   int32(lineNum),
				ErrorMessage: fmt.Sprintf("failed to read CSV record: %v", err),
			})
			continue
		}

		if len(record) > 50000 { // Max 50,000 customers
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("CSV contains more than 50,000 customers"))
		}

		var customerID uuid.UUID
		if idCol != -1 && record[idCol] != "" {
			customerID, err = uuid.Parse(record[idCol])
			if err != nil {
				importErrors = append(importErrors, &bookv1.ImportError{
					LineNumber:   int32(lineNum),
					ErrorMessage: fmt.Sprintf("invalid customer ID: %v", err),
				})
				continue
			}
		} else {
			customerID = uuid.New()
		}

		// 値が空のキーは保存しない — 空の差し込み変数を持たせても本文では
		// 空文字になるだけで、JSONB を無駄に太らせる。
		fields := make(map[string]string, len(customCols))
		for _, c := range customCols {
			if v := getStringValue(record, c.index); v != "" {
				fields[c.key] = v
			}
		}
		customFields, cfErr := customfields.MarshalSanitized(fields)
		if cfErr != nil {
			// 到達しない想定 (map[string]string の marshal) だが、
			// 1 行の失敗で取り込み全体を落とさない。
			importErrors = append(importErrors, &bookv1.ImportError{
				LineNumber:   int32(lineNum),
				ErrorMessage: fmt.Sprintf("failed to encode custom fields: %v", cfErr),
			})
			continue
		}

		customer := db.CreateCustomerParams{
			ID:           customerID,
			BookID:       bookID,
			Phone:        getStringValue(record, phoneCol),
			Category:     getStringValue(record, categoryCol),
			Name:         getStringValue(record, nameCol),
			Corporation:  getStringValue(record, corporationCol),
			Address:      getStringValue(record, addressCol),
			Memo:         getStringValue(record, memoCol),
			Mail:         getStringValue(record, mailCol),
			CustomFields: customFields,
		}
		customersToInsert = append(customersToInsert, customerRow{params: customer, lineNum: int32(lineNum)})
	}

	// Insert customers one by one and collect successfully inserted ones for
	// a single ES bulk index call at the end.
	now := time.Now()
	var importedCount int32
	docsToIndex := make([]search.CustomerDoc, 0, len(customersToInsert))
	for _, row := range customersToInsert {
		customer := row.params
		_, err := s.queries.CreateCustomer(ctx, customer)
		if err != nil {
			importErrors = append(importErrors, &bookv1.ImportError{
				LineNumber:   row.lineNum,
				ErrorMessage: fmt.Sprintf("failed to insert customer: %v", err),
			})
			continue
		}
		importedCount++
		docsToIndex = append(docsToIndex, search.NewCustomerDoc(
			customer.ID,
			customer.BookID,
			customer.Name,
			customer.Corporation,
			customer.Address,
			customer.Memo,
			customer.Phone,
			customer.Category,
			now,
		))
	}

	// Bulk index the newly inserted customers. Degraded mode: warn and continue
	// on failure so CSV import never fails due to ES unavailability.
	if idxErr := s.indexer.BulkIndex(ctx, docsToIndex); idxErr != nil {
		log.Warn().Err(idxErr).Int("count", len(docsToIndex)).Msg("failed to bulk index imported customers")
	}

	return connect.NewResponse(&bookv1.ImportBookResponse{
		BookId:        bookID.String(),
		ImportedCount: importedCount,
		FailedCount:   int32(len(importErrors)),
		Errors:        importErrors,
	}), nil
}

// canonicalCSVColumns は Customer の固定列に吸われるヘッダ名。ここに挙げた
// 名前は custom_fields には入れない (mail と email は片方しか使われなくても
// 「メールアドレス列」であることに変わりはないので両方とも予約する)。
var canonicalCSVColumns = map[string]bool{
	"id": true, "phone": true, "category": true, "name": true,
	"corporation": true, "address": true, "memo": true,
	"mail": true, "email": true,
}

// customFieldColumn は CSV の未知列 1 つ ぶんの「正規化済みキー → 列位置」。
type customFieldColumn struct {
	key   string
	index int
}

// buildCustomFieldColumns は CSV ヘッダから custom_fields 用の列を選ぶ。
//
//   - canonical 列は除外
//   - 列名は customfields.NormalizeKey で正規化 (小文字化・[a-z0-9_] 以外は '_')。
//     日本語だけの列名など、正規化してキーにならないものは従来通り無視する
//   - 同じキーに潰れる列が複数あれば最初の 1 つだけ採用
//   - 最大 customfields.MaxFields 列 (超過分は無視) — 素性の知れない営業
//     リストで JSONB が肥大するのを防ぐ
func buildCustomFieldColumns(header []string) []customFieldColumn {
	var cols []customFieldColumn
	seen := make(map[string]bool, len(header))
	for i, raw := range header {
		lower := strings.ToLower(strings.TrimSpace(raw))
		if canonicalCSVColumns[lower] {
			continue
		}
		key, ok := customfields.NormalizeKey(raw)
		if !ok || seen[key] || canonicalCSVColumns[key] {
			continue
		}
		if len(cols) >= customfields.MaxFields {
			log.Warn().Int("max", customfields.MaxFields).Str("column", raw).
				Msg("book: too many custom field columns in CSV — extra columns ignored")
			break
		}
		seen[key] = true
		cols = append(cols, customFieldColumn{key: key, index: i})
	}
	return cols
}

func getStringValue(record []string, colIndex int) string {
	if colIndex != -1 && colIndex < len(record) {
		return strings.TrimSpace(record[colIndex])
	}
	return ""
}
