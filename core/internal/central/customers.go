package central

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	// premiumMonthlyCap is how many washes Premium includes each calendar month. It twins
	// siteagent.PremiumMonthlyCap, which the lane enforces, so a change to one changes the other.
	premiumMonthlyCap          = 8
	maxRegisterPlateBytes      = 4 << 10
	rejectedRegistrationReason = "the body needs a plate of 2 to 7 letters and digits, with spaces or hyphens allowed"
	unreadableCustomerCars     = "central could not read this customer's cars"
)

type customerAPI struct {
	store *Store
	month billingMonth
}

type customerResponse struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	Cars []carPlanResponse `json:"cars"`
}

type carPlanResponse struct {
	Plate string  `json:"plate"`
	Plan  *string `json:"plan"`
}

type customerCarResponse struct {
	Plate              string  `json:"plate"`
	Plan               *string `json:"plan"`
	WashesUsed         int     `json:"washes_used"`
	MonthlyCap         int     `json:"monthly_cap"`
	ResetsOn           string  `json:"resets_on"`
	PrepaidWashesReady int     `json:"prepaid_washes_ready"`
}

type customerCarDetailResponse struct {
	customerCarResponse
	Washes []plateWashResponse `json:"washes"`
}

type registerPlateRequest struct {
	Plate string `json:"plate"`
}

func newCustomerAPI(store *Store) *customerAPI {
	return &customerAPI{store: store, month: newBillingMonth()}
}

func (c *customerAPI) handleGetCustomers(w http.ResponseWriter, r *http.Request) {
	customers, err := c.store.Customers(r.Context())
	if err != nil {
		slog.Error("customer listing failed", "error", err)
		http.Error(w, "central could not list customers", http.StatusInternalServerError)
		return
	}
	response := make([]customerResponse, 0, len(customers))
	for _, customer := range customers {
		cars := make([]carPlanResponse, 0, len(customer.Cars))
		for _, car := range customer.Cars {
			cars = append(cars, carPlanResponse{Plate: car.Plate, Plan: optionalString(string(car.Plan))})
		}
		response = append(response, customerResponse{ID: customer.ID, Name: customer.Name, Cars: cars})
	}
	writeJSON(w, http.StatusOK, response)
}

func (c *customerAPI) handleGetCars(w http.ResponseWriter, r *http.Request) {
	customerID := r.PathValue("id")
	cars, err := c.store.CustomerCars(r.Context(), customerID, c.month.start())
	if errors.Is(err, ErrUnknownCustomer) {
		http.Error(w, "central holds no customer with this id", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("customer cars read failed", "customer_id", customerID, "error", err)
		http.Error(w, unreadableCustomerCars, http.StatusInternalServerError)
		return
	}
	resetsOn := c.resetsOn()
	response := make([]customerCarResponse, 0, len(cars))
	for _, car := range cars {
		response = append(response, newCustomerCarResponse(car, resetsOn))
	}
	writeJSON(w, http.StatusOK, response)
}

func (c *customerAPI) handleGetCar(w http.ResponseWriter, r *http.Request) {
	plate, isWellFormed := canonicalPlate(r.PathValue("plate"))
	if !isWellFormed {
		http.Error(w, rejectedPlateReason, http.StatusBadRequest)
		return
	}
	c.answerCar(w, r, r.PathValue("id"), plate, http.StatusOK)
}

// handlePostCar registers a plate to the customer. Registering one the customer already holds answers 200
// with the car, so a retried request is safe.
func (c *customerAPI) handlePostCar(w http.ResponseWriter, r *http.Request) {
	customerID := r.PathValue("id")
	var body registerPlateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRegisterPlateBytes))
	if err := decoder.Decode(&body); err != nil || decoder.More() {
		http.Error(w, rejectedRegistrationReason, http.StatusBadRequest)
		return
	}
	plate, isWellFormed := canonicalPlate(body.Plate)
	if !isWellFormed {
		http.Error(w, rejectedRegistrationReason, http.StatusBadRequest)
		return
	}

	isNew, err := c.store.RegisterPlate(r.Context(), customerID, plate)
	switch {
	case errors.Is(err, ErrUnknownCustomer):
		http.Error(w, "central holds no customer with this id", http.StatusNotFound)
		return
	case errors.Is(err, ErrPlateTaken):
		slog.Info("plate registration refused", "customer_id", customerID, "reason", "plate taken")
		http.Error(w, "this plate is registered to another owner", http.StatusConflict)
		return
	case err != nil:
		slog.Error("plate registration failed", "customer_id", customerID, "error", err)
		http.Error(w, "central could not register this plate", http.StatusInternalServerError)
		return
	}
	slog.Info("plate registered", "customer_id", customerID, "is_new", isNew)

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	c.answerCar(w, r, customerID, plate, status)
}

func (c *customerAPI) answerCar(w http.ResponseWriter, r *http.Request, customerID, plate string, status int) {
	car, err := c.store.CustomerCar(r.Context(), customerID, plate, c.month.start())
	if errors.Is(err, ErrUnknownPlate) {
		http.Error(w, "this customer holds no car with this plate", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("customer car read failed", "customer_id", customerID, "error", err)
		http.Error(w, unreadableCustomerCars, http.StatusInternalServerError)
		return
	}
	response := customerCarDetailResponse{
		customerCarResponse: newCustomerCarResponse(car, c.resetsOn()),
		Washes:              make([]plateWashResponse, 0, len(car.Washes)),
	}
	for _, wash := range car.Washes {
		response.Washes = append(response.Washes, plateWashResponse{AdmittedAt: wash.AdmittedAt, SiteID: wash.SiteID})
	}
	writeJSON(w, status, response)
}

// resetsOn is the date the monthly cap next resets, as a date in the billing zone.
func (c *customerAPI) resetsOn() string {
	return c.month.nextStart().Format(time.DateOnly)
}

func newCustomerCarResponse(car CustomerCar, resetsOn string) customerCarResponse {
	return customerCarResponse{
		Plate:              car.Plate,
		Plan:               optionalString(string(car.Plan)),
		WashesUsed:         car.WashesUsed,
		MonthlyCap:         premiumMonthlyCap,
		ResetsOn:           resetsOn,
		PrepaidWashesReady: car.PrepaidWashesReady,
	}
}
