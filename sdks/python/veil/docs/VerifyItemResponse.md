# VerifyItemResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**item** | **str** |  | 
**verified** | **bool** |  | 
**mode** | **str** |  | 

## Example

```python
from veil.models.verify_item_response import VerifyItemResponse

# TODO update the JSON string below
json = "{}"
# create an instance of VerifyItemResponse from a JSON string
verify_item_response_instance = VerifyItemResponse.from_json(json)
# print the JSON string representation of the object
print(VerifyItemResponse.to_json())

# convert the object into a dict
verify_item_response_dict = verify_item_response_instance.to_dict()
# create an instance of VerifyItemResponse from a dict
verify_item_response_from_dict = VerifyItemResponse.from_dict(verify_item_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


