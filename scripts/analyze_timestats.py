#!/usr/bin/python3

"""
This script is just a helper to aggregate the timestat file written by the regolancer
"""

import csv
import argparse

def aggregate_csv_times(files):
    total_query = 0.0
    total_send = 0.0

    for file in files:
        with open(file, mode='r') as csvfile:
            reader = csv.DictReader(csvfile)
            for row in reader:
                total_query += float(row['timeQueryRoute'])
                total_send += float(row['timeSendToRoute'])

    return total_query, total_send

def print_aggregated_results(total_query, total_send):
    sum_query = int(total_query)
    query_days = sum_query / (24 * 3600)

    sum_send = int(total_send)
    send_days = sum_send / (24 * 3600)

    total_time = total_query + total_send
    ratio_query = (total_query / total_time) * 100 if total_time else 0
    ratio_send = (total_send / total_time) * 100 if total_time else 0

    print(f"Time for QueryRoute:  {sum_query:>15}s {query_days:>6.2f}d {ratio_query:>6.2f}%")
    print(f"Time for SendToRoute: {sum_send:>15}s {send_days:>6.2f}d {ratio_send:>6.2f}%")

def main():
    parser = argparse.ArgumentParser(description="Aggregate times from multiple CSV files.")
    parser.add_argument('--files', type=str, nargs='+', required=True, help='Paths to CSV files')
    args = parser.parse_args()

    total_query, total_send = aggregate_csv_times(args.files)
    print_aggregated_results(total_query, total_send)

if __name__ == "__main__":
    main()
