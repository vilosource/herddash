package main

func helper(st interface{ Append(any) error }) { st.Append(nil) }
