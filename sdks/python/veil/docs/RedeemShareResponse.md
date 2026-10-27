# RedeemShareResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** |  | [optional] 
**blob_b64** | **str** | Base64 of the sealed envelope — returned exactly once. | 
**meta** | **Dict[str, object]** |  | [optional] 

## Example

```python
from veil.models.redeem_share_response import RedeemShareResponse

# TODO update the JSON string below
json = "{}"
# create an instance of RedeemShareResponse from a JSON string
redeem_share_response_instance = RedeemShareResponse.from_json(json)
# print the JSON string representation of the object
print(RedeemShareResponse.to_json())

# convert the object into a dict
redeem_share_response_dict = redeem_share_response_instance.to_dict()
# create an instance of RedeemShareResponse from a dict
redeem_share_response_from_dict = RedeemShareResponse.from_dict(redeem_share_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


