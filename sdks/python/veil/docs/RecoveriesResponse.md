# RecoveriesResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**requests** | [**List[RecoveryRequest]**](RecoveryRequest.md) |  | 

## Example

```python
from veil.models.recoveries_response import RecoveriesResponse

# TODO update the JSON string below
json = "{}"
# create an instance of RecoveriesResponse from a JSON string
recoveries_response_instance = RecoveriesResponse.from_json(json)
# print the JSON string representation of the object
print(RecoveriesResponse.to_json())

# convert the object into a dict
recoveries_response_dict = recoveries_response_instance.to_dict()
# create an instance of RecoveriesResponse from a dict
recoveries_response_from_dict = RecoveriesResponse.from_dict(recoveries_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


