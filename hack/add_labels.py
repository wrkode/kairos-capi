#!/usr/bin/env python3
"""
Add contract version labels to Cluster API CRDs.

This script adds the required CAPI contract version labels to CRD metadata:
- cluster.x-k8s.io/provider: kairos
- cluster.x-k8s.io/v1beta2: v1beta2

These labels are required by Cluster API for provider discovery and contract version
compatibility checking. See: https://cluster-api.sigs.k8s.io/developer/providers/contracts/overview.html#api-version-labels

The script is idempotent - running it multiple times produces the same result.
"""
import sys
import yaml
from pathlib import Path


def validate_crd(crd):
    """Validate that the loaded YAML is a valid CRD."""
    if not isinstance(crd, dict):
        raise ValueError("CRD file must contain a YAML dictionary")
    if crd.get('kind') != 'CustomResourceDefinition':
        raise ValueError("File must be a CustomResourceDefinition")
    if 'metadata' not in crd:
        raise ValueError("CRD must have metadata field")
    if 'name' not in crd.get('metadata', {}):
        raise ValueError("CRD metadata must have a name field")
    return True


def add_labels_to_crd(filepath):
    """
    Add contract version labels to a CRD file, preserving field order.
    
    Args:
        filepath: Path to the CRD YAML file
        
    Raises:
        FileNotFoundError: If the file doesn't exist
        ValueError: If the file is not a valid CRD
        yaml.YAMLError: If the file contains invalid YAML
    """
    filepath = Path(filepath)
    
    if not filepath.exists():
        raise FileNotFoundError(f"CRD file not found: {filepath}")
    
    # Read and parse YAML
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            crd = yaml.safe_load(f)
    except yaml.YAMLError as e:
        raise ValueError(f"Invalid YAML in {filepath}: {e}") from e
    
    if crd is None:
        raise ValueError(f"Empty YAML file: {filepath}")
    
    # Validate CRD structure
    validate_crd(crd)
    
    # Ensure metadata exists
    if 'metadata' not in crd:
        crd['metadata'] = {}
    
    metadata = crd['metadata']
    
    # Initialize labels if they don't exist
    if 'labels' not in metadata:
        metadata['labels'] = {}
    
    # Add/update contract version labels (idempotent operation)
    required_labels = {
        'cluster.x-k8s.io/provider': 'kairos',
        'cluster.x-k8s.io/v1beta2': 'v1beta2'
    }
    metadata['labels'].update(required_labels)
    
    # Preserve field order: annotations, name, labels, then any other keys
    # This matches controller-gen output order for consistency
    ordered_metadata = {}
    
    # Add keys in desired order if they exist
    for key in ['annotations', 'name', 'labels']:
        if key in metadata:
            ordered_metadata[key] = metadata[key]
    
    # Add any remaining keys (preserving their original order)
    for key, value in metadata.items():
        if key not in ordered_metadata:
            ordered_metadata[key] = value
    
    crd['metadata'] = ordered_metadata
    
    # Write back preserving formatting
    # Use a very wide width to minimize line wrapping differences from controller-gen
    try:
        with open(filepath, 'w', encoding='utf-8') as f:
            yaml.dump(
                crd,
                f,
                default_flow_style=False,
                sort_keys=False,
                allow_unicode=True,
                width=float('inf')  # Disable line wrapping to match controller-gen output
            )
    except IOError as e:
        raise IOError(f"Failed to write CRD file {filepath}: {e}") from e


def main():
    """Main entry point."""
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <crd-file>", file=sys.stderr)
        print("Adds CAPI contract version labels to a CRD file.", file=sys.stderr)
        sys.exit(1)
    
    filepath = sys.argv[1]
    
    try:
        add_labels_to_crd(filepath)
    except (FileNotFoundError, ValueError, IOError) as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Unexpected error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()

