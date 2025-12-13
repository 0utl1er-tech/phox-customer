package contact

import (
	"context"
	"encoding/csv"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

func (s *ContactService) ImportContactWithCustomer(
	ctx context.Context,
	req *connect.Request[contactv1.ImportContactWithCustomerRequest],
) (*connect.Response[contactv1.ImportContactWithCustomerResponse], error) {
	csvData := req.Msg.CsvData
	if csvData == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	reader := csv.NewReader(strings.NewReader(csvData))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if len(records) < 2 {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	// ヘッダー解析
	headers := records[0]
	headerIndex := make(map[string]int)
	for i, h := range headers {
		headerIndex[strings.ToLower(strings.TrimSpace(h))] = i
	}

	// customer_id は必須
	customerIdIdx, hasCustomerId := headerIndex["customer_id"]
	if !hasCustomerId {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	// 任意カラムのインデックス取得
	nameIdx := getIndex(headerIndex, "name")
	sexIdx := getIndex(headerIndex, "sex")
	phoneIdx := getIndex(headerIndex, "phone")
	mailIdx := getIndex(headerIndex, "mail")
	faxIdx := getIndex(headerIndex, "fax")

	var importedCount int32
	var failedCount int32
	var errors []*contactv1.ImportContactError

	for i, record := range records[1:] {
		lineNumber := int32(i + 2) // 1-indexed + header row

		// customer_id取得
		if customerIdIdx >= len(record) || strings.TrimSpace(record[customerIdIdx]) == "" {
			errors = append(errors, &contactv1.ImportContactError{
				LineNumber:   lineNumber,
				ErrorMessage: "customer_idは必須です",
			})
			failedCount++
			continue
		}

		customerIdStr := strings.TrimSpace(record[customerIdIdx])
		customerId, err := uuid.Parse(customerIdStr)
		if err != nil {
			errors = append(errors, &contactv1.ImportContactError{
				LineNumber:   lineNumber,
				ErrorMessage: "customer_idが不正なUUID形式です",
			})
			failedCount++
			continue
		}

		// Contact作成（name, sexを直接Contactに格納）
		contactId := uuid.New()
		name := getValue(record, nameIdx)
		sex := getValue(record, sexIdx)
		phone := getValue(record, phoneIdx)
		mail := getValue(record, mailIdx)
		fax := getValue(record, faxIdx)

		_, err = s.queries.CreateContact(ctx, db.CreateContactParams{
			ID:         contactId,
			CustomerID: customerId,
			Name:       name,
			Sex:        sex,
			Phone:      phone,
			Mail:       mail,
			Fax:        fax,
		})
		if err != nil {
			errors = append(errors, &contactv1.ImportContactError{
				LineNumber:   lineNumber,
				ErrorMessage: "連絡先の作成に失敗しました: " + err.Error(),
			})
			failedCount++
			continue
		}

		importedCount++
	}

	return connect.NewResponse(&contactv1.ImportContactWithCustomerResponse{
		ImportedCount: importedCount,
		FailedCount:   failedCount,
		Errors:        errors,
	}), nil
}

func getIndex(headerIndex map[string]int, key string) int {
	if idx, ok := headerIndex[key]; ok {
		return idx
	}
	return -1
}

func getValue(record []string, idx int) string {
	if idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}
