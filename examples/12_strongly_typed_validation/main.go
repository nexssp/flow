package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/validation"
	"github.com/nexssp/validation/validate"
)

type OrderItem struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
	PriceUSD int64  `json:"price_usd_cents"`
}

func (i OrderItem) Validate() error {
	if err := validate.Required(i.SKU, "sku"); err != nil {
		return err
	}
	if err := validate.Min(i.Quantity, 1, "quantity"); err != nil {
		return err
	}
	if err := validate.Min(i.PriceUSD, 1, "price_usd_cents"); err != nil {
		return err
	}
	return nil
}

type CheckoutReq struct {
	CustomerID string      `json:"customer_id"`
	Email      string      `json:"email"`
	Items      []OrderItem `json:"items"`
}

func (c CheckoutReq) Validate() error {
	if err := validate.Required(c.CustomerID, "customer_id"); err != nil {
		return err
	}
	if err := validate.Required(c.Email, "email"); err != nil {
		return err
	}
	if !validation.IsValidEmail(c.Email) {
		return validate.Required("", "email_format_invalid")
	}
	if len(c.Items) == 0 {
		return validate.Required("", "items_cannot_be_empty")
	}
	for i, item := range c.Items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("items[%d]: %w", i, err)
		}
	}
	return nil
}

type CheckoutRes struct {
	OrderID     string    `json:"order_id"`
	TotalCents  int64     `json:"total_cents"`
	ProcessedAt time.Time `json:"processed_at"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// No defer cancel – we call cancel manually on exit paths.

	processCheckout := action.New("checkout.process", func(_ context.Context, req CheckoutReq) (CheckoutRes, error) {
		var total int64
		for _, item := range req.Items {
			total += int64(item.Quantity) * item.PriceUSD
		}

		return CheckoutRes{
			OrderID:     "ord_z100a99",
			TotalCents:  total,
			ProcessedAt: time.Now().UTC(),
		}, nil
	})

	checkoutAction := validation.AutoValidate(processCheckout).Build()

	validReq := CheckoutReq{
		CustomerID: "cust_99",
		Email:      "engineering@nexss.com",
		Items: []OrderItem{
			{SKU: "SSD-1TB-PRO", Quantity: 2, PriceUSD: 12000},
			{SKU: "RAM-32GB-DDR5", Quantity: 4, PriceUSD: 8500},
		},
	}

	fmt.Println("🚀 Processing valid structured checkout...")
	res, err := checkoutAction.Do(ctx, validReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Unexpected checkout failure: %v\n", err)
		cancel()
		os.Exit(1)
	}
	fmt.Printf("✅ Order Executed Successfully! OrderID: %s, Total: $%.2f\n\n", res.OrderID, float64(res.TotalCents)/100)

	invalidReq := CheckoutReq{
		CustomerID: "",
		Email:      "invalid-email-format",
		Items: []OrderItem{
			{SKU: "NVME-SATA", Quantity: 0, PriceUSD: 4500},
		},
	}

	fmt.Println("🚀 Sending invalid payload to verify validation guard...")
	_, err = checkoutAction.Do(ctx, invalidReq)
	if err == nil {
		fmt.Fprintln(os.Stderr, "❌ Guard Failure: Action executed despite containing invalid data parameters.")
		cancel()
		os.Exit(1)
	}

	var appErr *xerr.AppError
	if errors.As(err, &appErr) {
		fmt.Printf("✅ Validation Interceptor Correctly Blocked Request:\n   Kind: %s\n   Message: %s\n   Cause: %v\n",
			appErr.Kind, appErr.Message, appErr.Cause)
	} else {
		fmt.Printf("✅ Blocked with error: %v\n", err)
	}

	cancel()
}
