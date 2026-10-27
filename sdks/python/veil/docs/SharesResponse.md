# SharesResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**shares** | [**List[Share]**](Share.md) |  | 

## Example

```python
from veil.models.shares_response import SharesResponse

# TODO update the JSON string below
json = "{}"
# create an instance of SharesResponse from a JSON string
shares_response_instance = SharesResponse.from_json(json)
# print the JSON string representation of the object
print(SharesResponse.to_json())

# convert the object into a dict
shares_response_dict = shares_response_instance.to_dict()
# create an instance of SharesResponse from a dict
shares_response_from_dict = SharesResponse.from_dict(shares_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


