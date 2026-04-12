package esclient

// CustomerIndexMapping is the full ES index definition (settings + mappings)
// for the `phox_customers` index. Fields:
//
//   - customer_id, book_id, category, prefecture, phone: keyword (exact match,
//     aggregations, facets)
//   - name, corporation, address, memo: text with the `ja_kuromoji` analyzer
//     for Japanese tokenization
//   - phone_text: text analyzed with `standard` (splits on `-` so partial
//     number searches like `1234` hit `090-1234-5678`)
//   - updated_at: date
//
// The analyzer chain mirrors Elasticsearch's documented recommended Japanese
// analyzer pipeline (kuromoji_tokenizer + baseform + POS filter + stop +
// number + stemmer + lowercase).
const CustomerIndexMapping = `{
  "settings": {
    "analysis": {
      "analyzer": {
        "ja_kuromoji": {
          "type": "custom",
          "tokenizer": "kuromoji_tokenizer",
          "filter": [
            "kuromoji_baseform",
            "kuromoji_part_of_speech",
            "ja_stop",
            "kuromoji_number",
            "kuromoji_stemmer",
            "lowercase"
          ]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "customer_id": { "type": "keyword" },
      "book_id":     { "type": "keyword" },
      "category":    { "type": "keyword" },
      "prefecture":  { "type": "keyword" },
      "phone":       { "type": "keyword" },
      "phone_text":  { "type": "text", "analyzer": "standard" },
      "name":        { "type": "text", "analyzer": "ja_kuromoji" },
      "corporation": { "type": "text", "analyzer": "ja_kuromoji" },
      "address":     { "type": "text", "analyzer": "ja_kuromoji" },
      "memo":        { "type": "text", "analyzer": "ja_kuromoji" },
      "updated_at":  { "type": "date" }
    }
  }
}`
