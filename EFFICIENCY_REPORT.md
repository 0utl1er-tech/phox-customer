# Code Efficiency Report

This report documents several areas in the codebase that could be more efficient.

## 1. Individual INSERT Statements in ImportBook (High Impact)

**File:** `internal/service/book/import_book.go` (lines 140-160)

**Issue:** When importing customers from a CSV file, the code inserts each customer one by one in a loop, making N separate database round trips for N customers.

```go
for i, customer := range customersToInsert {
    _, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{...})
}
```

**Impact:** For large CSV imports (e.g., 10,000 customers), this results in 10,000 separate database queries, significantly slowing down the import process.

**Recommendation:** Use batch inserts with a single INSERT statement containing multiple VALUES, or use PostgreSQL's COPY command for bulk data loading.

## 2. Individual INSERT Statements in ImportContactWithCustomer (High Impact)

**File:** `internal/service/contact/import_contact_with_customer.go` (lines 58-109)

**Issue:** Similar to ImportBook, contacts are inserted one by one in a loop instead of using batch operations.

**Impact:** Same performance degradation as above for large contact imports.

**Recommendation:** Implement batch inserts for contacts as well.

## 3. Missing Slice Pre-allocation in ListAllContacts (Low Impact)

**File:** `internal/service/contact/list_all_contacts.go` (line 29)

**Issue:** The contacts slice is declared without pre-allocation:
```go
var contacts []*contactv1.ContactWithCustomer
```

Other list functions in the codebase correctly pre-allocate:
```go
customerList := make([]*customerv1.Customer, 0, len(customers))
```

**Impact:** Without pre-allocation, the slice may need to be reallocated multiple times as elements are appended, causing unnecessary memory allocations and copies.

**Recommendation:** Pre-allocate the slice with the known capacity: `contacts := make([]*contactv1.ContactWithCustomer, 0, len(rows))`

## 4. Redundant Struct Copy in ImportBook (Low Impact)

**File:** `internal/service/book/import_book.go` (lines 142-152)

**Issue:** The code creates a `db.CreateCustomerParams` struct, stores it in `customersToInsert`, then creates another identical struct when calling `CreateCustomer`:

```go
customer := db.CreateCustomerParams{...}
customersToInsert = append(customersToInsert, customer)
// Later:
_, err := s.queries.CreateCustomer(ctx, db.CreateCustomerParams{
    ID:          customer.ID,
    BookID:      customer.BookID,
    // ... redundant copy of all fields
})
```

**Impact:** Unnecessary struct creation and field copying.

**Recommendation:** Use the existing `customer` variable directly: `s.queries.CreateCustomer(ctx, customer)`

## 5. Use of uuid.MustParse Without Error Handling (Medium Impact)

**Files:** Multiple service files including `list_customer.go`, `get_permit.go`

**Issue:** Several places use `uuid.MustParse()` which will panic if the UUID is invalid:
```go
bookID := uuid.MustParse(req.Msg.BookId)
```

**Impact:** Invalid input from clients could cause the server to panic instead of returning a proper error response.

**Recommendation:** Use `uuid.Parse()` with proper error handling to return appropriate error responses to clients.

## Summary

| Issue | Impact | Effort to Fix |
|-------|--------|---------------|
| Batch inserts in ImportBook | High | Medium |
| Batch inserts in ImportContactWithCustomer | High | Medium |
| Slice pre-allocation in ListAllContacts | Low | Low |
| Redundant struct copy in ImportBook | Low | Low |
| uuid.MustParse error handling | Medium | Low |
