# BillingCheckout


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**checkout_url** | **str** | Fresh hosted Vortex checkout URL for this org — navigate immediately, single-use | 

## Example

```python
from veil.models.billing_checkout import BillingCheckout

# TODO update the JSON string below
json = "{}"
# create an instance of BillingCheckout from a JSON string
billing_checkout_instance = BillingCheckout.from_json(json)
# print the JSON string representation of the object
print(BillingCheckout.to_json())

# convert the object into a dict
billing_checkout_dict = billing_checkout_instance.to_dict()
# create an instance of BillingCheckout from a dict
billing_checkout_from_dict = BillingCheckout.from_dict(billing_checkout_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


