# BillingView


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**plan** | **str** | Plan as enforced by the Use gate | 
**used** | **int** | Uses consumed in the current window | 
**included** | **int** | Free allowance per window; null when unlimited (metering off or paid plan) | 
**window_start** | **datetime** |  | 
**window_end** | **datetime** |  | 
**upgrade_url** | **str** | Vortex checkout/portal link for capped owners | [optional] 

## Example

```python
from veil.models.billing_view import BillingView

# TODO update the JSON string below
json = "{}"
# create an instance of BillingView from a JSON string
billing_view_instance = BillingView.from_json(json)
# print the JSON string representation of the object
print(BillingView.to_json())

# convert the object into a dict
billing_view_dict = billing_view_instance.to_dict()
# create an instance of BillingView from a dict
billing_view_from_dict = BillingView.from_dict(billing_view_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


