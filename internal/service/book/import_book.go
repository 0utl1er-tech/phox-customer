package book

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
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

	// Expected columns (ignoring pic and leader for now due to schema complexity)
	// The user can provide an 'id' column, otherwise it will be auto-generated.
	// Other columns are optional and will default to empty string if not present.
	idCol := -1
	if idx, ok := columnMap["id"]; ok {
		idCol = idx
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

	var customersToInsert []db.CreateCustomerParams
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

		customer := db.CreateCustomerParams{
			ID:          customerID,
			BookID:      bookID,
			Category:    getStringValue(record, categoryCol),
			Name:        getStringValue(record, nameCol),
			Corporation: getStringValue(record, corporationCol),
			Address:     getStringValue(record, addressCol),
			Memo:        getStringValue(record, memoCol),
		}
		customersToInsert = append(customersToInsert, customer)
	}

	// Insert customers one by one
	if len(customersToInsert) > 0 {
		for i, customer := range customersToInsert {
			_, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
				ID:          customer.ID,
				BookID:      customer.BookID,
				Category:    customer.Category,
				Name:        customer.Name,
				Corporation: customer.Corporation,
				Address:     customer.Address,
				Memo:        customer.Memo,
			})
			if err != nil {
				importErrors = append(importErrors, &bookv1.ImportError{
					LineNumber:   int32(i + 2),
					ErrorMessage: fmt.Sprintf("failed to insert customer (index %d): %v", i, err),
				})
			}
		}
	}

	// Calculate imported count
	importedCount := int32(len(customersToInsert) - len(importErrors))

	return connect.NewResponse(&bookv1.ImportBookResponse{
		BookId:        bookID.String(),
		ImportedCount: importedCount,
		FailedCount:   int32(len(importErrors)),
		Errors:        importErrors,
	}), nil
}

func getStringValue(record []string, colIndex int) string {
	if colIndex != -1 && colIndex < len(record) {
		return strings.TrimSpace(record[colIndex])
	}
	return ""
}
