package helpers

import (
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"stockbackend/clients/http_client"
	"stockbackend/types"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"gopkg.in/mgo.v2/bson"
)

// Helper function to match header titles
func MatchHeader(cellValue string, patterns []string) bool {
	normalizedValue := NormalizeString(cellValue)
	for _, pattern := range patterns {
		matched, _ := regexp.MatchString(pattern, normalizedValue)
		if matched {
			return true
		}
	}
	return false
}

// Helper function to normalize strings
func NormalizeString(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func CheckInstrumentName(input string) bool {
	// Regular expression to match "Name of the Instrument" or "Name of Instrument"
	pattern := `Name of (the )?Instrument`

	// Compile the regex
	re := regexp.MustCompile(pattern)

	// Check if the pattern matches the input string
	return re.MatchString(input)
}

func ToFloat(value interface{}) float64 {
	if str, ok := value.(string); ok {
		// Remove commas from the string
		cleanStr := strings.ReplaceAll(str, ",", "")

		// Check if the string contains a percentage symbol
		if strings.Contains(cleanStr, "%") {
			// Remove the percentage symbol
			cleanStr = strings.ReplaceAll(cleanStr, "%", "")
			// Convert to float and divide by 100 to get the decimal equivalent
			f, err := strconv.ParseFloat(cleanStr, 64)
			if err != nil {
				zap.L().Error("Error converting to float64", zap.Error(err))
				return 0.0
			}
			return f / 100.0
		}

		// Parse the cleaned string to float
		f, err := strconv.ParseFloat(cleanStr, 64)
		if err != nil {
			zap.L().Error("Error converting to float64", zap.Error(err))
			return 0.0
		}
		return f
	}
	return 0.0
}

func ToStringArray(value interface{}) []string {
	if arr, ok := value.(primitive.A); ok {
		var strArr []string
		for _, v := range arr {
			if str, ok := v.(string); ok {
				strArr = append(strArr, str)
			}
		}
		return strArr
	}
	return []string{}
}

func GetMarketCapCategory(marketCapValue string) string {

	cleanMarketCapValue := strings.ReplaceAll(marketCapValue, ",", "")

	marketCap, err := strconv.ParseFloat(cleanMarketCapValue, 64) // 64-bit float
	if err != nil {
		log.Println("Failed to convert market cap to integer: %v", err)
	}
	// Define market cap categories in crore (or billions as per comment)
	if marketCap >= 20000 {
		return "Large Cap"
	} else if marketCap >= 5000 && marketCap < 20000 {
		return "Mid Cap"
	} else if marketCap < 5000 {
		return "Small Cap"
	}
	return "Unknown Category"
}

// rateStock calculates the final stock rating

func RateStock(stock map[string]interface{}) float64 {
	// zap.L().Info("Stock data", zap.Any("stock", stock))
	stockData := types.Stock{
		Name:          stock["name"].(string),
		PE:            ToFloat(stock["stockPE"]),
		MarketCap:     ToFloat(stock["marketCap"]),
		DividendYield: ToFloat(stock["dividendYield"]),
		ROCE:          ToFloat(stock["roce"]),
		Cons:          ToStringArray(stock["cons"]),
		Pros:          ToStringArray(stock["pros"]),
	}
	// zap.L().Info("Stock data", zap.Any("stock", stockData))
	// zap.L().Info("Stock data", zap.Any("stock", stockData))
	peerComparisonScore := compareWithPeers(stockData, stock["peers"]) * 0.5
	trendScore := AnalyzeTrend(stockData, stock["quarterlyResults"]) * 0.4
	// prosConsScore := prosConsAdjustment(stock) * 0.1
	// zap.L().Info("Peer comparison score", zap.Float64("peerComparisonScore", peerComparisonScore))

	finalScore := peerComparisonScore + trendScore
	finalScore = math.Round(finalScore*100) / 100
	return finalScore
}

// compareWithPeers calculates a peer comparison score
func compareWithPeers(stock types.Stock, peers interface{}) float64 {
	peerScore := 0.0
	var medianScore float64

	if arr, ok := peers.(primitive.A); ok {
		// Ensure there are enough peers to compare
		if len(arr) < 2 {
			zap.L().Warn("Not enough peers to compare")
			return 0.0
		}

		for _, peerRaw := range arr[:len(arr)-1] {
			peer := peerRaw.(bson.M)

			// Parse peer values to float64
			peerPE := ParseFloat(peer["pe"])
			peerMarketCap := ParseFloat(peer["market_cap"])
			peerDividendYield := ParseFloat(peer["div_yield"])
			peerROCE := ParseFloat(peer["roce"])
			peerQuarterlySales := ParseFloat(peer["sales_qtr"])
			peerQuarterlyProfit := ParseFloat(peer["np_qtr"])

			// Example scoring logic
			if stock.PE < peerPE {
				peerScore += 10
			} else {
				peerScore += math.Max(0, 10-(stock.PE-peerPE))
			}

			if stock.MarketCap > peerMarketCap {
				peerScore += 5
			}

			if stock.DividendYield > peerDividendYield {
				peerScore += 5
			}

			if stock.ROCE > peerROCE {
				peerScore += 10
			}

			if stock.QuarterlySales > peerQuarterlySales {
				peerScore += 5
			}

			if stock.QuarterlyProfit > peerQuarterlyProfit {
				peerScore += 10
			}
		}
		medianRaw := arr[len(arr)-1]
		median := medianRaw.(bson.M)

		// Parse median values to float64
		medianPE := ParseFloat(median["pe"])
		medianMarketCap := ParseFloat(median["market_cap"])
		medianDividendYield := ParseFloat(median["div_yield"])
		medianROCE := ParseFloat(median["roce"])
		medianQuarterlySales := ParseFloat(median["sales_qtr"])
		medianQuarterlyProfit := ParseFloat(median["np_qtr"])

		// Adjust score based on median comparison
		if stock.PE < medianPE {
			peerScore += 5
		} else {
			peerScore += math.Max(0, 5-(stock.PE-medianPE))
		}

		if stock.MarketCap > medianMarketCap {
			peerScore += 3
		}

		if stock.DividendYield > medianDividendYield {
			peerScore += 3
		}

		if stock.ROCE > medianROCE {
			peerScore += 5
		}

		if stock.QuarterlySales > medianQuarterlySales {
			peerScore += 2
		}

		if stock.QuarterlyProfit > medianQuarterlyProfit {
			peerScore += 5
		}

		// Normalize by the number of peers (excluding the median)
		peerCount := len(arr) - 1
		if peerCount > 0 {
			return peerScore / float64(peerCount)
		}

		// Normalize by the number of peers excluding the last element
	}

	// Combine peerScore with medianScore (example: giving 10% weight to the median)
	finalScore := (peerScore * 0.9) + (medianScore * 0.1)

	return finalScore
}

// Helper function to convert values from map to float64
func ParseFloat(value interface{}) float64 {
	switch v := value.(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0.0
		}
		return f
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0.0
	}
}
func AnalyzeTrend(stock types.Stock, pastData interface{}) float64 {
	trendScore := 0.0
	comparisons := 0 // Keep track of the number of comparisons

	// Ensure pastData is in bson.M format
	if data, ok := pastData.(bson.M); ok {
		for _, quarterData := range data {
			// zap.L().Info("Processing quarter", zap.String("quarter", key))

			// Process the quarter data if it's a primitive.A (array of quarter maps)
			if quarterArray, ok := quarterData.(primitive.A); ok {
				var prevElem bson.M
				for i, elem := range quarterArray {
					if elemMap, ok := elem.(bson.M); ok {
						// zap.L().Info("Processing quarter element", zap.Any("element", elemMap))

						// Only perform comparisons starting from the second element
						if i > 0 && prevElem != nil {
							// zap.L().Info("Comparing with previous element", zap.Any("previous", prevElem), zap.Any("current", elemMap))

							// Iterate over the keys in the current quarter and compare with previous quarter
							for key, v := range elemMap {
								if prevVal, ok := prevElem[key]; ok {
									// Compare consecutive values for the same key
									if ToFloat(v) > ToFloat(prevVal) {
										trendScore += 5
									} else if ToFloat(v) < ToFloat(prevVal) {
										trendScore -= 5
									}
									// Increment comparisons for each valid comparison
									comparisons++
								}
							}
						}
						// Update previous element for next iteration
						prevElem = elemMap
					}
				}
			}
		}
	}

	// Normalize the score by dividing it by the number of comparisons
	if comparisons > 0 {
		return trendScore / float64(comparisons)
	}
	return 0.0 // Return 0 if no comparisons were made
}

// prosConsAdjustment calculates score adjustments based on pros and cons
func ProsConsAdjustment(stock types.Stock) float64 {
	adjustment := 0.0

	// Adjust score based on pros
	// for _, pro := range stock.Pros {
	// zap.L().Info("Pro", zap.String("pro", pro)) // This line is optional, just showing how we could use 'pro'
	adjustment += ToFloat(1.0 * len(stock.Pros))
	// }

	// Adjust score based on cons
	// for _, con := range stock.Cons {
	// zap.L().Info("Con", zap.String("con", con)) // This line is optional, just showing how we could use 'con'
	adjustment -= ToFloat(1.0 * len(stock.Cons))
	// }/

	return adjustment
}

func ParsePeersTable(doc *goquery.Document, selector string) []map[string]string {
	var peers []map[string]string
	headers := []string{}

	// Extract table headers
	doc.Find(fmt.Sprintf("%s table thead tr th", selector)).Each(func(i int, s *goquery.Selection) {
		headers = append(headers, strings.TrimSpace(s.Text()))
	})

	// Parse each row of the peers table
	doc.Find(fmt.Sprintf("%s table tbody tr", selector)).Each(func(i int, row *goquery.Selection) {
		peerData := map[string]string{}
		row.Find("td").Each(func(j int, cell *goquery.Selection) {
			if j < len(headers) {
				peerData[headers[j]] = strings.TrimSpace(cell.Text())
			}
		})
		peers = append(peers, peerData)
	})

	return peers
}

func FetchPeerData(dataWarehouseID string) ([]map[string]string, error) {
	time.Sleep(1 * time.Second)
	peerURL := fmt.Sprintf(os.Getenv("COMPANY_URL")+"/api/company/%s/peers/", dataWarehouseID)

	// Create a new HTTP request
	req, err := http.NewRequest("GET", peerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request to peers API: %w", err)
	}

	// Add any required headers or cookies here
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching peers data from API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		zap.L().Error("Received non-200 response code", zap.Int("status_code", resp.StatusCode), zap.String("body", bodyString))
		return nil, fmt.Errorf("received non-200 response code from peers API: %d", resp.StatusCode)
	}

	// Parse the HTML response
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing HTML response: %w", err)
	}

	var peersData []map[string]string
	var medianData map[string]string

	// Parse peers data from the table rows
	doc.Find("tr[data-row-company-id]").Each(func(index int, item *goquery.Selection) {
		peer := make(map[string]string)

		peer["name"] = item.Find("td.text a").Text()
		peer["current_price"] = strings.TrimSpace(item.Find("td").Eq(2).Text())
		peer["pe"] = strings.TrimSpace(item.Find("td").Eq(3).Text())
		peer["market_cap"] = strings.TrimSpace(item.Find("td").Eq(4).Text())
		peer["div_yield"] = strings.TrimSpace(item.Find("td").Eq(5).Text())
		peer["np_qtr"] = strings.TrimSpace(item.Find("td").Eq(6).Text())
		peer["qtr_profit_var"] = strings.TrimSpace(item.Find("td").Eq(7).Text())
		peer["sales_qtr"] = strings.TrimSpace(item.Find("td").Eq(8).Text())
		peer["qtr_sales_var"] = strings.TrimSpace(item.Find("td").Eq(9).Text())
		peer["roce"] = strings.TrimSpace(item.Find("td").Eq(10).Text())

		peersData = append(peersData, peer)
	})

	// Parse median data from the footer of the table
	doc.Find("tfoot tr").Each(func(index int, item *goquery.Selection) {
		medianData = make(map[string]string)
		medianData["company_count"] = strings.TrimSpace(item.Find("td").Eq(1).Text())
		medianData["current_price"] = strings.TrimSpace(item.Find("td").Eq(2).Text())
		medianData["pe"] = strings.TrimSpace(item.Find("td").Eq(3).Text())
		medianData["market_cap"] = strings.TrimSpace(item.Find("td").Eq(4).Text())
		medianData["div_yield"] = strings.TrimSpace(item.Find("td").Eq(5).Text())
		medianData["np_qtr"] = strings.TrimSpace(item.Find("td").Eq(6).Text())
		medianData["qtr_profit_var"] = strings.TrimSpace(item.Find("td").Eq(7).Text())
		medianData["sales_qtr"] = strings.TrimSpace(item.Find("td").Eq(8).Text())
		medianData["qtr_sales_var"] = strings.TrimSpace(item.Find("td").Eq(9).Text())
		medianData["roce"] = strings.TrimSpace(item.Find("td").Eq(10).Text())
	})

	peersData = append(peersData, medianData)
	return peersData, nil
}

func ParseTableData(section *goquery.Selection, tableSelector string) map[string]interface{} {
	table := section.Find(tableSelector)
	if table.Length() == 0 {
		return nil
	}

	// Extract months/years from table headers
	headers := []string{}
	table.Find("thead th").Each(func(i int, th *goquery.Selection) {
		headers = append(headers, strings.TrimSpace(th.Text()))
	})

	// Extract table rows and values
	data := make(map[string]interface{})
	table.Find("tbody tr").Each(func(i int, tr *goquery.Selection) {
		rowKey := strings.TrimSpace(tr.Find("td.text").Text())
		rowValues := []string{}
		tr.Find("td").Each(func(i int, td *goquery.Selection) {
			if i > 0 { // Skip the first column which is the row key
				rowValues = append(rowValues, strings.TrimSpace(td.Text()))
			}
		})
		data[rowKey] = rowValues
	})

	return data
}

func ParseShareholdingPattern(section *goquery.Selection) map[string]interface{} {
	shareholdingData := make(map[string]interface{})

	// Extract quarterly data
	quarterlyData := ParseTable(section.Find("div#quarterly-shp"))
	if len(quarterlyData) > 0 {
		shareholdingData["quarterly"] = quarterlyData
	}

	// Extract yearly data
	yearlyData := ParseTable(section.Find("div#yearly-shp"))
	if len(yearlyData) > 0 {
		shareholdingData["yearly"] = yearlyData
	}

	return shareholdingData
}

func ParseTable(tableDiv *goquery.Selection) []map[string]interface{} {
	var tableData []map[string]interface{}

	// Get the headers (dates) from the table
	var headers []string
	tableDiv.Find("table thead th").Each(func(index int, header *goquery.Selection) {
		if index > 0 { // Skip the first column header (e.g., "Promoters", "FIIs", etc.)
			headers = append(headers, strings.TrimSpace(header.Text()))
		}
	})

	// Iterate over each row in the table body
	tableDiv.Find("table tbody tr").Each(func(index int, row *goquery.Selection) {
		rowData := make(map[string]interface{})

		// Extract the row label (e.g., "Promoters", "FIIs", etc.)
		label := strings.TrimSpace(row.Find("td.text").Text())
		rowData["category"] = label

		// Extract values for each date (column)
		values := make(map[string]string)
		row.Find("td").Each(func(i int, cell *goquery.Selection) {
			if i > 0 && i <= len(headers) { // Ensure we are within the bounds of the headers array
				date := headers[i-1] // Corresponding date (column header)
				values[date] = strings.TrimSpace(cell.Text())
			}
		})

		rowData["values"] = values
		tableData = append(tableData, rowData)
	})

	return tableData
}

func FetchCompanyData(url string) (map[string]interface{}, error) {
	body, err := http_client.GetCompanyPage(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the company page: %v", err)
	}

	// Parse the HTML content of the company page
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the HTML content: %v", err)
	}
	// Extract data-warehouse-id
	companyData := make(map[string]interface{})

	dataWarehouseID, exists := doc.Find("div[data-warehouse-id]").Attr("data-warehouse-id")
	if exists {
		peerData, err := FetchPeerData(dataWarehouseID)
		if err == nil {
			companyData["peers"] = peerData
		}
	}

	// Extract the data we need
	// Extract data as specified
	doc.Find("li.flex.flex-space-between[data-source='default']").Each(func(index int, item *goquery.Selection) {
		key := strings.TrimSpace(item.Find("span.name").Text())

		// Extract value text and clean it up
		value := strings.TrimSpace(item.Find("span.nowrap.value").Text())
		value = strings.ReplaceAll(value, "\n", "") // Remove newlines
		value = strings.ReplaceAll(value, " ", "")  // Remove extra spaces

		// Extract the numeric value if it exists inside the nested span and clean it up
		number := item.Find("span.number").Text()
		if number != "" {
			number = strings.TrimSpace(number)
			value = strings.ReplaceAll(value, number, number) // Ensure no extra spaces around numbers
		}

		// Remove currency symbols and units from value
		value = strings.ReplaceAll(value, "₹", "")
		value = strings.ReplaceAll(value, "Cr.", "")
		value = strings.ReplaceAll(value, "%", "")

		// Add to company data
		companyData[key] = value

		// Print cleaned key-value pairs
		zap.L().Info("Company Data", zap.String("key", key), zap.String("value", value))
		log.Printf("%s: %s\n", key, value)
	})
	// Extract pros
	var pros []string
	doc.Find("div.pros ul li").Each(func(index int, item *goquery.Selection) {
		pro := strings.TrimSpace(item.Text())
		pros = append(pros, pro)
	})
	companyData["pros"] = pros

	// Extract cons
	var cons []string
	doc.Find("div.cons ul li").Each(func(index int, item *goquery.Selection) {
		con := strings.TrimSpace(item.Text())
		cons = append(cons, con)
	})
	companyData["cons"] = cons
	// Extract Quarterly Results
	quarterlyResults := make(map[string][]map[string]string)
	// Get the months (headers) from the table
	var months []string
	doc.Find("table.data-table thead tr th").Each(func(index int, item *goquery.Selection) {
		month := strings.TrimSpace(item.Text())
		if month != "" && month != "-" { // Skip empty or irrelevant headers
			months = append(months, month)
		}
	})

	// Iterate over each row in the tbody
	doc.Find("table.data-table tbody tr").Each(func(index int, row *goquery.Selection) {
		fieldName := strings.TrimSpace(row.Find("td.text").Text())
		var fieldData []map[string]string

		// Iterate over each column in the row
		row.Find("td").Each(func(colIndex int, col *goquery.Selection) {
			if colIndex > 0 && colIndex <= len(months) { // Ensure we are within the bounds of the months array
				value := strings.TrimSpace(col.Text())
				month := months[colIndex]
				fieldData = append(fieldData, map[string]string{
					month: value,
				})
			}
		})

		if len(fieldData) > 0 {
			quarterlyResults[fieldName] = fieldData
		}
	})

	companyData["quarterlyResults"] = quarterlyResults
	profitLossSection := doc.Find("section#profit-loss")
	if profitLossSection.Length() > 0 {
		companyData["profitLoss"] = ParseTableData(profitLossSection, "div[data-result-table]")
	}
	balanceSheetSection := doc.Find("section#balance-sheet")
	if balanceSheetSection.Length() > 0 {
		companyData["balanceSheet"] = ParseTableData(balanceSheetSection, "div[data-result-table]")
	}
	shareHoldingPattern := doc.Find("section#shareholding")
	if shareHoldingPattern.Length() > 0 {
		companyData["shareholdingPattern"] = ParseShareholdingPattern(shareHoldingPattern)
	}

	ratiosSection := doc.Find("section#ratios")
	if ratiosSection.Length() > 0 {
		companyData["ratios"] = ParseTableData(ratiosSection, "div[data-result-table]")
	}
	cashFlowsSection := doc.Find("section#cash-flow")
	if cashFlowsSection.Length() > 0 {
		companyData["cashFlows"] = ParseTableData(cashFlowsSection, "div[data-result-table]")
	}
	return companyData, nil
}