package model

// Required input columns per dataset. Optional columns may be absent from the header.
var (
	ReserveRequired    = []string{"asset_type", "amount", "counterparty", "is_immediately_available"}
	SupplyRequired     = []string{"as_of", "circulating_supply"}
	RedemptionRequired = []string{"request_ts", "amount", "status"}
)
