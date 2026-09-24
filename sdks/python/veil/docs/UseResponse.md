# UseResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**decision** | **str** |  | 
**reason** | **str** |  | [optional] 
**approval_id** | **str** |  | [optional] 
**request_id** | **str** | The filed approval request when decision is need_approval. Owners resolve it via /v1/requests/{id}. | [optional] 
**request_expires_at** | **datetime** | When the filed ask dies unanswered. Agents may keep retrying until then. | [optional] 
**status** | **int** |  | [optional] 
**headers** | **Dict[str, List[str]]** | Upstream response headers, including Content-Type and Content-Encoding. | [optional] 
**body** | **str** | Upstream body with vault secrets scrubbed. Present when the body is valid UTF-8. | [optional] 
**body_b64** | **bytes** | Base64 upstream body with vault secrets scrubbed. Present when the body is not valid UTF-8 (gzip, images, protobuf). | [optional] 

## Example

```python
from veil.models.use_response import UseResponse

# TODO update the JSON string below
json = "{}"
# create an instance of UseResponse from a JSON string
use_response_instance = UseResponse.from_json(json)
# print the JSON string representation of the object
print(UseResponse.to_json())

# convert the object into a dict
use_response_dict = use_response_instance.to_dict()
# create an instance of UseResponse from a dict
use_response_from_dict = UseResponse.from_dict(use_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


