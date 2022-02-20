// Package alert defines routes for alerts
package alert

import (
	"sort"
	"strconv"
	"net/http"
	"github.com/shopspring/decimal"
	"github.com/gorilla/mux"
	"github.com/w0rp/pricewarp/internal/template"
	"github.com/w0rp/pricewarp/internal/database"
	"github.com/w0rp/pricewarp/internal/model"
	"github.com/w0rp/pricewarp/internal/session"
	"github.com/w0rp/pricewarp/internal/route/util"
)

var alertQuery = `
select
	crypto_alert.id,
	above,
	time,
	sent,
	value,
	from_currency.id,
	from_currency.ticker,
	from_currency.name,
	to_currency.id,
	to_currency.ticker,
	to_currency.name
from crypto_alert
inner join crypto_currency as from_currency
on from_currency.id = crypto_alert."from"
inner join crypto_currency as to_currency
on to_currency.id = crypto_alert."to"
`

func ScanAlert(row database.Row, alert *model.Alert) error {
	return row.Scan(
		&alert.ID,
		&alert.Above,
		&alert.Time,
		&alert.Sent,
		&alert.Value,
		&alert.From.ID,
		&alert.From.Ticker,
		&alert.From.Name,
		&alert.To.ID,
		&alert.To.Ticker,
		&alert.To.Name,
	)
}

var currencyQuery = `select id, ticker, name from crypto_currency `

func ScanCurrency(row database.Row, currency *model.Currency) error {
	return row.Scan(&currency.ID, &currency.Ticker, &currency.Name)
}

func loadAlertList(conn *database.Conn, userID int, alertList *[]model.Alert) error {
	rows, err := conn.Query(alertQuery + "where user_id = $1 order by time", userID)

	if err != nil {
		return err
	}

	*alertList = make([]model.Alert, 0, 1)
	var alert model.Alert

	for rows.Next() {
		if err := ScanAlert(rows, &alert); err != nil {
			return err
		}

		*alertList = append(*alertList, alert)
	}

	return nil
}

func loadCurrencyList(conn *database.Conn, currencyList *[]model.Currency) error {
	*currencyList = make([]model.Currency, 0, 500)

	rows, err := conn.Query(currencyQuery + "order by name")

	if err != nil {
		return err
	}

	var currency model.Currency

	for rows.Next() {
		if err := ScanCurrency(rows, &currency); err != nil {
			return err
		}

		*currencyList = append(*currencyList, currency)
	}

	return nil
}

func loadUser(conn *database.Conn, writer http.ResponseWriter, request *http.Request, user *model.User) bool {
	found, err := session.LoadUserFromSession(conn, request, user)

	if err != nil {
		util.RespondInternalServerError(writer, err)

		return false
	}

	return found
}

type AlertPageData struct {
	User model.User
	Alert model.Alert
	FromCurrencyList []model.Currency
	ToCurrencyList []model.Currency
}

type AlertListPageData struct {
	AlertPageData
	AlertList []model.Alert
}

var toCurrencyTickers = []string {
	"USD",
	"GBP",
	"BTC",
}

type ByTickerOrder []model.Currency

func IndexOfString(array []string, element string) int {
	for i, v := range array {
		if element == v {
			return i
		}
	}

	return -1
}

func (a ByTickerOrder) Len() int {
	return len(a)
}

func (a ByTickerOrder) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a ByTickerOrder) Less(i, j int) bool {
	leftIndex := IndexOfString(toCurrencyTickers, a[i].Ticker)
	rightIndex := IndexOfString(toCurrencyTickers, a[j].Ticker)

	return leftIndex < rightIndex
}

func BuildToCurrencyList(currencyList []model.Currency) []model.Currency {
	toCurrencyList := make([]model.Currency, 0, len(toCurrencyTickers))

	for _, currency := range currencyList {
		for _, ticker := range toCurrencyTickers {
			if currency.Ticker == ticker {
				toCurrencyList = append(toCurrencyList, currency)
				break
			}
		}
	}

	sort.Sort(ByTickerOrder(toCurrencyList))

	return toCurrencyList
}

func HandleAlertList(conn *database.Conn, writer http.ResponseWriter, request *http.Request) {
	data := AlertListPageData{}
	data.Alert.Above = true

	if !loadUser(conn, writer, request, &data.User) {
		http.Redirect(writer, request, "/login", http.StatusFound)

		return
	}

	if err := loadAlertList(conn, data.User.ID, &data.AlertList); err != nil {
		util.RespondInternalServerError(writer, err)

		return
	}

	if err := loadCurrencyList(conn, &data.FromCurrencyList); err != nil {
		util.RespondInternalServerError(writer, err)

		return
	}

	data.ToCurrencyList = BuildToCurrencyList(data.FromCurrencyList)
	template.Render(template.AlertList, writer, data)
}

func loadAlertForRequest(
	conn *database.Conn,
	writer http.ResponseWriter,
	request *http.Request,
	user *model.User,
	alert *model.Alert,
) bool {
	alertID, err := strconv.Atoi(mux.Vars(request)["id"])

	if err != nil {
		util.RespondNotFound(writer)

		return false
	}

	row := conn.QueryRow(alertQuery + " where user_id = $1 and crypto_alert.id = $2", user.ID, alertID)

	if err := ScanAlert(row, alert); err != nil {
		if err == database.ErrNoRows {
			util.RespondNotFound(writer)
		} else {
			util.RespondInternalServerError(writer, err)
		}

		return false
	}

	return true
}

func HandleAlert(conn *database.Conn, writer http.ResponseWriter, request *http.Request) {
	data := AlertPageData{}
	data.Alert.Above = true

	if !loadUser(conn, writer, request, &data.User) {
		http.Redirect(writer, request, "/login", http.StatusFound)

		return
	}

	if loadAlertForRequest(conn, writer, request, &data.User, &data.Alert) {
		if err := loadCurrencyList(conn, &data.FromCurrencyList); err != nil {
			util.RespondInternalServerError(writer, err)
		} else {
			data.ToCurrencyList = BuildToCurrencyList(data.FromCurrencyList)
			template.Render(template.AlertList, writer, data)
		}
	}
}

func loadAlertFromForm(
	conn *database.Conn,
	writer http.ResponseWriter,
	request *http.Request,
	alert *model.Alert,
) bool {
	var err error
	request.ParseForm()

	from, err := strconv.Atoi(request.Form.Get("from"))

	if err != nil {
		util.RespondValidationError(writer, "Invalid from currency ID")

		return false
	}

	to, err := strconv.Atoi(request.Form.Get("to"))

	if err != nil {
		util.RespondValidationError(writer, "Invalid to currency ID")

		return false
	}

	if from == to {
		util.RespondValidationError(writer, "From and to currencies cannot be the same")

		return false
	}

	value, err := decimal.NewFromString(request.Form.Get("value"))

	if err != nil {
		util.RespondValidationError(writer, "Invalid value")

		return false
	}

	direction := request.Form.Get("direction")

	if direction != "above" && direction != "below" {
		util.RespondValidationError(writer, "Invalid direction")

		return false
	}

	alert.Value = value

	if direction == "above" {
		alert.Above = true
	} else {
		alert.Above = false
	}

	var row database.Row

	row = conn.QueryRow(currencyQuery + "where id = $1", from)

	if err := ScanCurrency(row, &alert.From); err != nil {
		util.RespondInternalServerError(writer, err)

		return false
	}

	row = conn.QueryRow(currencyQuery + "where id = $1", to)

	if err := ScanCurrency(row, &alert.To); err != nil {
		util.RespondInternalServerError(writer, err)

		return false
	}

	return true
}

func HandleSubmitAlert(conn *database.Conn, writer http.ResponseWriter, request *http.Request) {
	var user model.User
	var alert model.Alert

	if !loadUser(conn, writer, request, &user) {
		util.RespondForbidden(writer)

		return
	}

	if loadAlertFromForm(conn, writer, request, &alert) {
		insertSQL := `
		insert into crypto_alert(user_id, above, time, sent, value, "from", "to")
		values ($1, $2, NOW(), false, $3, $4, $5)
		`

		if err := conn.Exec(insertSQL, user.ID, alert.Above, alert.Value, alert.From.ID, alert.To.ID); err != nil {
			util.RespondInternalServerError(writer, err)
		} else {
			http.Redirect(writer, request, "/alert", http.StatusFound)
		}
	}
}

func HandleUpdateAlert(conn *database.Conn, writer http.ResponseWriter, request *http.Request) {
	var user model.User
	var alert model.Alert

	if !loadUser(conn, writer, request, &user) {
		util.RespondForbidden(writer)

		return
	}

	if loadAlertForRequest(conn, writer, request, &user, &alert) && loadAlertFromForm(conn, writer, request, &alert) {
		updateSQL := `
		update crypto_alert
		set above = $2,
			time = NOW(),
			sent = false,
			value = $3,
			"from" = $4,
			"to" = $5
		where id = $1
		`

		if err := conn.Exec(updateSQL, alert.ID, alert.Above, alert.Value, alert.From.ID, alert.To.ID); err != nil {
			util.RespondInternalServerError(writer, err)
		} else {
			http.Redirect(writer, request, "/alert", http.StatusFound)
		}
	}
}

func HandleDeleteAlert(conn *database.Conn, writer http.ResponseWriter, request *http.Request) {
	var user model.User
	var alert model.Alert

	if !loadUser(conn, writer, request, &user) {
		util.RespondForbidden(writer)

		return
	}

	if loadAlertForRequest(conn, writer, request, &user, &alert) {
		if err := conn.Exec("delete from crypto_alert where id = $1", alert.ID); err != nil {
			util.RespondInternalServerError(writer, err)
		} else {
			writer.WriteHeader(http.StatusNoContent)
		}
	}
}
