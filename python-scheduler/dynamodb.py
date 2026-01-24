import boto3
import pandas as pd
import logging
from boto3.dynamodb.conditions import Key, Attr

logger = logging.getLogger("scheduler.dynamodb")


class DynamoDB:
    def __init__(self, access_key, secret_key, endpoint, region="ap-northeast-2"):
        self.endpoint = endpoint
        self.resource = boto3.resource(
            "dynamodb",
            region_name=region,
            aws_access_key_id=access_key,
            aws_secret_access_key=secret_key,
            # endpoint_url=endpoint # Uncomment if using custom endpoint like localstack or specific gateway
        )

    def query(
        self,
        TableName,
        FilterExpression=None,
        ProjectionExpression=None,
        ExpressionAttributeNames=None,
    ):
        table = self.resource.Table(TableName)
        try:
            # Note: This is a Scan operation if no KeyConditionExpression is provided.
            # Real query requires KeyConditionExpression on Primary Key / GSI.
            # Using Scan for flexibility based on user snippet implying broad filtering.

            scan_kwargs = {}
            if FilterExpression:
                scan_kwargs["FilterExpression"] = FilterExpression
            if ProjectionExpression:
                scan_kwargs["ProjectionExpression"] = ProjectionExpression
            if ExpressionAttributeNames:
                scan_kwargs["ExpressionAttributeNames"] = ExpressionAttributeNames

            response = table.scan(**scan_kwargs)
            data = response.get("Items", [])

            while "LastEvaluatedKey" in response:
                scan_kwargs["ExclusiveStartKey"] = response["LastEvaluatedKey"]
                response = table.scan(**scan_kwargs)
                data.extend(response.get("Items", []))

            return {"Items": data}
        except Exception as e:
            logger.error(f"DynamoDB Query Error: {e}")
            return {"Items": []}


def load_column_info(table_name, force=None):
    # Mock implementation - In real scenario, this might fetch schema
    return {"table_name": table_name}


def build_expressions(table_name, schema, custom_columns, conditions):
    """
    Builds FilterExpression and ProjectionExpression from conditions and columns.
    """
    filter_expr = None

    # Simple condition builder (AND logic)
    for col, cond in conditions.items():
        # cond format example: "= 'P9T'", "> 20260101"
        # Parsing this strictly needs more logic.
        # Here we assume direct equality for simplicity or use Attr if valid.

        # Strip operator simple parsing
        val = cond.strip().lstrip("=><").strip().strip("'")

        attr = Attr(col)
        new_filter = None

        if ">" in cond:
            new_filter = attr.gt(val)
        elif "<" in cond:
            new_filter = attr.lt(val)
        else:
            new_filter = attr.eq(val)

        if filter_expr is None:
            filter_expr = new_filter
        else:
            filter_expr = filter_expr & new_filter

    # Projection
    # DynamoDB reserved words handling needed for ProjectionExpression?
    # For now simply join
    proj_expr = ", ".join(custom_columns) if custom_columns else None

    # Handle reserved words only if needed (e.g. #ts for timestamp)
    attr_names = {}
    if custom_columns:
        safe_cols = []
        for col in custom_columns:
            # Very basic reserved word handling example
            if col in ["date", "time", "year", "month", "day", "group"]:
                safe_col = f"#{col}"
                attr_names[safe_col] = col
                safe_cols.append(safe_col)
            else:
                safe_cols.append(col)
        proj_expr = ", ".join(safe_cols)

    return {
        "TableName": table_name,
        "FilterExpression": filter_expr,
        "ProjectionExpression": proj_expr,
        "ExpressionAttributeNames": attr_names if attr_names else None,
    }


def items2df(items, schema, custom_columns, conditions=None):
    if not items:
        # Return empty DF with correct columns
        return pd.DataFrame(columns=custom_columns or [])

    df = pd.DataFrame(items)
    # Ensure all requested columns exist
    if custom_columns:
        for col in custom_columns:
            if col not in df.columns:
                df[col] = None
        # Reorder/Select
        df = df[custom_columns]

    return df
