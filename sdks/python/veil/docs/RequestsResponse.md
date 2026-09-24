# RequestsResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**requests** | [**List[ApprovalRequest]**](ApprovalRequest.md) |  | 

## Example

```python
from veil.models.requests_response import RequestsResponse

# TODO update the JSON string below
json = "{}"
# create an instance of RequestsResponse from a JSON string
requests_response_instance = RequestsResponse.from_json(json)
# print the JSON string representation of the object
print(RequestsResponse.to_json())

# convert the object into a dict
requests_response_dict = requests_response_instance.to_dict()
# create an instance of RequestsResponse from a dict
requests_response_from_dict = RequestsResponse.from_dict(requests_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


