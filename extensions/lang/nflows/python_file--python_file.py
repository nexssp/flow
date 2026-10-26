import json
import sys

values = json.load(sys.stdin)["values"]
print(json.dumps({"value": sum(values), "count": len(values)}))
